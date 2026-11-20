package update

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const restartStagePrefix = ".xdev-restart-"
const restartPayloadName = "payload.exe"

var errRestartUnsupported = errors.New("prepared restart updates are only supported on Windows")

// ErrPreparedUpdateInvalid means a new PrepareForRestart is required.
var ErrPreparedUpdateInvalid = errors.New("prepared update is no longer valid")

// PreparedUpdate owns a verified, unpublished update. The exported fields are
// display metadata; installation uses the private snapshot, not those fields.
// Do not copy a PreparedUpdate. Use Discard to abandon it before handoff.
type PreparedUpdate struct {
	Version    string
	TargetPath string
	AssetName  string

	mu    sync.Mutex
	stage string
	state preparedRestartState
	plan  restartPlan
}

type preparedRestartState uint8

const (
	restartPrepared preparedRestartState = iota
	restartHandedOff
	restartDiscarded
)

// RestartOptions describes the new process, excluding argv[0]. Pass only
// non-secret arguments. An empty WorkingDir uses the caller's current directory.
type RestartOptions struct {
	Args       []string
	WorkingDir string
}

// PrepareForRestart downloads and authenticates a Windows executable without
// changing the installed file. Success ends in ready, not completed.
func PrepareForRestart(ctx context.Context, result *CheckResult, target string, onProgress func(Progress)) (*PreparedUpdate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		return nil, errRestartUnsupported
	}
	asset, err := SelectedAsset(result)
	if err != nil {
		return nil, err
	}
	target, err = canonicalRestartPath(target)
	if err != nil {
		return nil, fmt.Errorf("resolve installed executable: %w", err)
	}
	if !strings.EqualFold(filepath.Ext(target), ".exe") {
		return nil, errors.New("installed restart target must be an .exe file")
	}
	oldHash, err := restartFileHash(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("hash installed executable: %w", err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), restartStagePrefix)
	if err != nil {
		return nil, fmt.Errorf("create restart staging directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()

	progress := &progressReporter{callback: onProgress}
	payload := filepath.Join(stage, restartPayloadName)
	raw := strings.EqualFold(filepath.Ext(asset.Name), ".exe")
	download := payload
	if !raw {
		download = filepath.Join(stage, "download.zip")
	}
	file, err := os.OpenFile(download, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create staged download: %w", err)
	}
	downloadErr := downloadReleaseArchive(ctx, asset, file, progress)
	if downloadErr == nil {
		downloadErr = file.Sync()
	}
	if err := file.Close(); downloadErr == nil {
		downloadErr = err
	}
	if downloadErr != nil {
		return nil, fmt.Errorf("download update: %w", downloadErr)
	}

	progress.phase("verifying")
	// Hold the same read handle across checksum and extraction; on Windows
	// this prevents another writer from changing the verified archive in place.
	verified, err := openRestartInput(download)
	if err != nil {
		return nil, fmt.Errorf("open staged download: %w", err)
	}
	defer verified.Close()
	payloadHash, err := verifyPreparedRestartDownload(ctx, result.Release, asset.Name, verified)
	if err != nil {
		return nil, fmt.Errorf("SHA256 verification failed: %w", err)
	}
	if !raw {
		progress.phase("extracting")
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		extracted, err := extractUpdateBinary(download, asset.Name, stage)
		if err != nil {
			return nil, fmt.Errorf("extract update binary: %w", err)
		}
		if err := os.Rename(extracted, payload); err != nil {
			return nil, fmt.Errorf("stage extracted executable: %w", err)
		}
		if err := verified.Close(); err != nil {
			return nil, err
		}
		if err := os.Remove(download); err != nil {
			return nil, fmt.Errorf("remove verified archive: %w", err)
		}
		payloadHash, err = restartFileHash(ctx, payload)
		if err != nil {
			return nil, fmt.Errorf("hash extracted executable: %w", err)
		}
	}
	if err := validateRestartPE(payload, runtime.GOARCH); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prepared := &PreparedUpdate{
		Version: result.LatestVersion, TargetPath: target, AssetName: asset.Name, stage: stage,
		plan: restartPlan{Target: target, OldSHA256: oldHash, PayloadSHA256: payloadHash},
	}
	keep = true
	progress.phase("ready")
	return prepared, nil
}

func verifyPreparedRestartDownload(ctx context.Context, release *ReleaseInfo, assetName string, file *os.File) (string, error) {
	asset, ok := findChecksumAsset(release)
	if !ok {
		return "", errors.New("no unique SHA256SUMS asset found in release")
	}
	data, err := downloadReleaseChecksums(ctx, asset)
	if err != nil {
		return "", fmt.Errorf("download SHA256SUMS: %w", err)
	}
	want, err := parseSHA256SUMS(string(data), assetName)
	if err != nil {
		return "", err
	}
	if !validRestartDigest(want) {
		return "", fmt.Errorf("invalid SHA-256 for %s", assetName)
	}
	got, err := hashRestartInput(ctx, file)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("hash mismatch for %s: expected %s, got %s", assetName, want, got)
	}
	return got, nil
}

// Discard removes this update's staging files. Once handed off, only the
// independent helper owns them. Repeated calls before handoff are harmless.
func (p *PreparedUpdate) Discard() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == restartHandedOff {
		return errors.New("restart helper already owns the prepared update")
	}
	if p.stage == "" {
		return nil
	}
	root, err := os.OpenRoot(filepath.Dir(p.plan.Target))
	if err != nil {
		return fmt.Errorf("open prepared update directory: %w", err)
	}
	defer root.Close()
	if err := root.RemoveAll(filepath.Base(p.stage)); err != nil {
		return fmt.Errorf("discard prepared update: %w", err)
	}
	p.stage, p.state = "", restartDiscarded
	return nil
}

func canonicalRestartPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func validRestartDigest(digest string) bool {
	data, err := hex.DecodeString(digest)
	return err == nil && len(data) == sha256.Size && strings.ToLower(digest) == digest
}

func hashRestartInput(ctx context.Context, r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, contextReader{ctx: ctx, r: r}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), ctx.Err()
}

func restartFileHash(ctx context.Context, path string) (string, error) {
	file, err := openRestartInput(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return hashRestartInput(ctx, file)
}

// verifyOpenedRestartInput authenticates the already pinned file object. Keep
// the caller's handle open through publication; never re-open by a mutable path.
func verifyOpenedRestartInput(ctx context.Context, file *os.File, expected string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	got, err := hashRestartInput(ctx, file)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("SHA-256 changed after preparation: %s", file.Name())
	}
	_, err = file.Seek(0, io.SeekStart)
	return err
}

func verifyRestartPathHash(ctx context.Context, path, expected string) error {
	if !canonicalRestartIdentity(path) {
		return fmt.Errorf("path is missing or no longer canonical: %s", path)
	}
	got, err := restartFileHash(ctx, path)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("SHA-256 changed after preparation: %s", path)
	}
	return nil
}

func validateRestartPE(path, arch string) error {
	file, err := openRestartInput(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return validateRestartPEInput(file, arch)
}

func validateRestartPEInput(input io.ReaderAt, arch string) error {
	file, err := pe.NewFile(input)
	if err != nil {
		return fmt.Errorf("read staged PE executable: %w", err)
	}
	defer file.Close()
	var machine uint16
	var goodHeader bool
	switch arch {
	case "amd64":
		machine = pe.IMAGE_FILE_MACHINE_AMD64
		_, goodHeader = file.OptionalHeader.(*pe.OptionalHeader64)
	case "arm64":
		machine = pe.IMAGE_FILE_MACHINE_ARM64
		_, goodHeader = file.OptionalHeader.(*pe.OptionalHeader64)
	case "386":
		machine = pe.IMAGE_FILE_MACHINE_I386
		_, goodHeader = file.OptionalHeader.(*pe.OptionalHeader32)
	default:
		return fmt.Errorf("unsupported Windows executable architecture %s", arch)
	}
	if file.Machine != machine || !goodHeader {
		return fmt.Errorf("staged PE architecture does not match windows/%s (machine 0x%x)", arch, file.Machine)
	}
	if file.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || file.Characteristics&pe.IMAGE_FILE_DLL != 0 || len(file.Sections) == 0 {
		return errors.New("staged PE is not an executable image")
	}
	return nil
}
