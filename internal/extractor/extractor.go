// Package extractor unpacks archives (zip, tar.gz, tar.xz, tar.bz2,
// tar.zst, 7z) to a target directory with security validation.
//
// # Supported Formats
//
//   - zip       (Windows embeddable Python, Node, etc.)
//   - tar       (raw .tar)
//   - tar.gz    (gzip-compressed tar)
//   - tar.xz    (xz-compressed tar)
//   - tar.bz2   (bzip2-compressed tar)
//   - tar.zst   (zstandard-compressed tar — via external zstd command)
//   - 7z        (7-Zip format — via external 7z command)
//   - exe       (NSIS/Inno Setup installers — extracted via 7z)
//   - pkg       (macOS installer packages — extracted via pkgutil)
//   - dmg       (macOS disk images — mounted and copied)
//
// # Security
//
// All extraction methods include zip-slip / tar-slip protection:
//   - extractZip and extractTarFromReader validate every file path with
//     isSafePath() before writing. If an unsafe path is detected, the
//     entire destination directory is removed (os.RemoveAll) and an
//     error is returned.
//   - extract7z runs a post-extraction validation (validateExtraction)
//     that walks the extracted tree and verifies all files are within
//     the destination directory. It also detects hard links (nlink>1),
//     device files, named pipes, and sockets.
//   - isSafePath uses case-insensitive comparison on Windows (EqualFold)
//     and checks for ".." path traversal via filepath.Rel.
//
// # Strip Parameter
//
// The strip parameter controls how many leading path components to remove.
// For example, with strip=1, "go1.22.0/bin/go" becomes "bin/go".
// This is commonly needed for Go and Python archives that wrap their
// contents in a top-level version-named directory.
//
// # Zip Bomb Protection
//
// maxDecompressSize (1 GiB per file) limits the total decompressed bytes
// to prevent zip bomb attacks. Files exceeding this limit are rejected.
package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// maxDecompressSize limits the total decompressed bytes per file to
// prevent zip bombs (1 GB per file).
const maxDecompressSize = 1 << 30 // 1 GiB

// Security (M-11): Total extraction size limit across all files
const maxTotalExtractSize int64 = 10 << 30 // 10 GiB total across all files

// Extract unpacks src to dst.
//
// strip is the number of leading path components to discard (used to
// skip the top-level directory inside most upstream tarballs).
func Extract(src, dst, format string, strip int) error {
	// F-20: strip comes straight from the manifest and was previously
	// unvalidated end to end. A negative value reached parts[strip:] and
	// panicked with "slice bounds out of range [-1:]", crashing the whole
	// install. Reject it here (defence in depth — stripPath also clamps).
	if strip < 0 {
		return fmt.Errorf("invalid strip value %d: must be >= 0", strip)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	switch strings.ToLower(format) {
	case "zip":
		return extractZip(src, dst, strip)
	case "tar":
		return extractTar(src, dst, strip, nil)
	case "tar.gz", "tgz":
		return extractTar(src, dst, strip, gzipReaderAdapter)
	case "tar.xz", "txz":
		return extractTarXZ(src, dst, strip)
	case "tar.bz2", "tbz2":
		return extractTar(src, dst, strip, bzip2ReaderAdapter)
	case "tar.zst", "tzst":
		return extractTarZst(src, dst, strip)
	case "7z":
		return extract7z(src, dst)
	case "exe":
		// NSIS/Inno Setup installers can be extracted by 7z
		return extract7z(src, dst)
	case "pkg":
		// macOS .pkg is xar-based; try 7z extraction as fallback
		return extract7z(src, dst)
	case "dmg":
		// macOS disk image — hdiutil attach → copy .app → detach
		return extractDmg(src, dst)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

// hdiutilCmd is the hdiutil command used by extractDmg. It is a package
// variable so tests can override it with a mock script (same pattern as
// the zstd/xz external-command fallbacks).
var hdiutilCmd = "hdiutil"

// defaultDittoCmd is the macOS ditto binary path used to copy .app bundles
// (preserves symlinks/metadata, unlike a plain recursive copy).
const defaultDittoCmd = "/usr/bin/ditto"

// dittoCmd is overridable by tests (same hook pattern as hdiutilCmd).
var dittoCmd = defaultDittoCmd

// detachDMG unmounts the image at mountDir: first a normal detach, then a
// forced detach (-force) if the first attempt fails. Returns a joined error
// when both attempts fail. 若 hdiutil 本身不可执行（exec.Error，如命令不存在），
// -force 同样无意义，直接返回原错误。
func detachDMG(mountDir string) error {
	first := exec.Command(hdiutilCmd, "detach", mountDir)
	util.HideWindow(first)
	if err := first.Run(); err == nil {
		return nil
	} else if isExecNotFound(err) {
		return fmt.Errorf("hdiutil detach %s: %w", mountDir, err)
	} else {
		force := exec.Command(hdiutilCmd, "detach", "-force", mountDir)
		util.HideWindow(force)
		err2 := force.Run()
		if err2 == nil {
			return nil
		}
		return fmt.Errorf("hdiutil detach %s failed (normal: %v; force: %v)", mountDir, err, err2)
	}
}

// isExecNotFound reports whether err indicates the command itself could not
// be executed (exec.Error: not found / not executable), as opposed to the
// command running and returning a non-zero exit.
func isExecNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

// extractDmg extracts a macOS .dmg disk image: attach it read-only with
// hdiutil, copy every .app bundle into dst, then detach.
//
// dmg 解包仅支持 macOS（依赖 hdiutil）。非 macOS 平台直接返回明确错误。
//
// 错误处理（023-A3-02）：
//   - attach 失败：尽力 detach 已知挂载点（可能部分挂载）后返回 attach 错误；
//   - detach 失败：先普通 detach，再尝试 detach -force，仍失败则并入返回值，
//     避免"解包成功但镜像残留挂载"。
func extractDmg(src, dst string) (retErr error) {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("dmg extraction is only supported on macOS (current: %s)", runtime.GOOS)
	}

	// 在系统临时目录创建挂载点（os.MkdirTemp 返回绝对路径，
	// hdiutil -mountpoint 要求绝对路径）。
	mountDir, err := os.MkdirTemp("", "xdev-dmg-*")
	if err != nil {
		return fmt.Errorf("create dmg mount dir: %w", err)
	}
	// 清理：无论成败都移除挂载点目录。
	defer os.RemoveAll(mountDir)

	// hdiutil attach -nobrowse -readonly -mountpoint <mountDir> <src>
	attach := exec.Command(hdiutilCmd, "attach", "-nobrowse", "-readonly", "-mountpoint", mountDir, src)
	util.HideWindow(attach)
	attach.Stdout = os.Stderr
	attach.Stderr = os.Stderr
	if err := attach.Run(); err != nil {
		// attach 失败：尽力卸载已知挂载点（可能部分挂载），仅记录不掩盖主错误。
		if dErr := detachDMG(mountDir); dErr != nil {
			log.Printf("[extractor] WARNING: cleanup detach after attach failure: %v", dErr)
		}
		return fmt.Errorf("hdiutil attach %s: %w", src, err)
	}
	// detach 在 defer 中执行，保证复制失败时镜像也被卸载；失败并入返回值。
	defer func() {
		if dErr := detachDMG(mountDir); dErr != nil {
			if retErr != nil {
				retErr = fmt.Errorf("%w; %v", retErr, dErr)
			} else {
				retErr = dErr
			}
		}
	}()

	// 复制挂载点下所有 .app 包到 dst（dmg 通常含一个 .app）。
	copied, err := copyAppsFromMount(mountDir, dst)
	if err != nil {
		return err
	}
	if copied == 0 {
		return fmt.Errorf("no .app bundle found in dmg %s", src)
	}
	return nil
}

// copyAppsFromMount copies every ".app" directory under mountDir into dst.
// It returns the number of .app bundles copied. 独立函数便于在非 macOS
// 平台用假挂载目录做单元测试（无需 hdiutil）。
func copyAppsFromMount(mountDir, dst string) (int, error) {
	entries, err := os.ReadDir(mountDir)
	if err != nil {
		return 0, fmt.Errorf("read dmg mount %s: %w", mountDir, err)
	}
	copied := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".app") {
			continue
		}
		if err := copyAppBundle(filepath.Join(mountDir, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return copied, fmt.Errorf("copy %s from dmg: %w", e.Name(), err)
		}
		copied++
	}
	return copied, nil
}

// copyAppBundle copies one .app bundle from the dmg mount into dstApp.
//
// macOS 上使用 /usr/bin/ditto（保留符号链接/元数据——Electron/IDE 包内含
// framework 符号链接，util.CopyDir 会跳过符号链接）。ditto 是 macOS 平台
// 先决条件：失败即返回错误，绝不静默降级为 CopyDir（否则 .app 会"复制成功"
// 但符号链接损坏，解包结果不可用）。仅非 darwin（且未覆盖 dittoCmd 测试
// 钩子）时使用 util.CopyDir 兜底（跨平台单测路径）。
func copyAppBundle(srcApp, dstApp string) error {
	if runtime.GOOS == "darwin" || dittoCmd != defaultDittoCmd {
		c := exec.Command(dittoCmd, srcApp, dstApp)
		util.HideWindow(c)
		c.Stdout = os.Stderr
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("ditto copy %s: %w", srcApp, err)
		}
		return nil
	}
	return util.CopyDir(srcApp, dstApp)
}

func extractZip(src, dst string, strip int) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	var totalExtracted int64
	for _, f := range r.File {
		rel, ok := stripPath(f.Name, strip)
		if !ok {
			continue
		}
		// Ticket 38 (P2-02): reject archive-relative absolute/UNC names at
		// the rel level — isSafePath now tolerates an UNC destination, so
		// this is where a "\\server\share" ENTRY is stopped.
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, `\\`) {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("zip slip: entry %q has an absolute/UNC name", f.Name)
		}
		out := filepath.Join(dst, rel)
		if !isSafePath(dst, out) {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("zip slip: path %q escapes destination %q", f.Name, dst)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(util.ToLongPath(out), 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(util.ToLongPath(filepath.Dir(out)), 0o755); err != nil {
			return err
		}
		n, err := unzipFile(f, util.ToLongPath(out))
		if err != nil {
			return err
		}
		totalExtracted += n
		// Security (M-11): Check total extraction size
		if totalExtracted > maxTotalExtractSize {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("total extraction size exceeds %d bytes limit (zip bomb protection)", maxTotalExtractSize)
		}
	}
	return nil
}

func unzipFile(f *zip.File, dst string) (int64, error) {
	in, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer in.Close()
	// Ticket 26 (A-08): use Mode().Perm() — the raw Mode() carries the
	// setuid/setgid/sticky and type bits, and passing them to OpenFile let a
	// hostile archive drop a setuid-root binary into the install tree.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, maxDecompressSize+1))
	if err != nil {
		return 0, err
	}
	if n > maxDecompressSize {
		return 0, fmt.Errorf("file %s exceeds max decompression size (%d bytes)", dst, maxDecompressSize)
	}
	return n, nil
}

func extractTar(src, dst string, strip int, decompress func(io.Reader) (io.ReadCloser, error)) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	var srcR io.Reader = f
	if decompress != nil {
		rc, err := decompress(srcR)
		if err != nil {
			return err
		}
		defer rc.Close()
		srcR = rc
	}
	return extractTarFromReader(srcR, dst, strip)
}

// extractTarXZ extracts a .tar.xz archive. It first tries the
// Go-native xz decoder via the github.com/xi2/xz package. If that
// is not available (not compiled in), it falls back to the system
// xz command.
func extractTarXZ(src, dst string, strip int) error {
	// Try native Go xz first
	if dec, err := newXZReader(src); err == nil {
		defer dec.Close()
		return extractTarFromReader(dec, dst, strip)
	}
	// Fallback: use system xz command
	return extractViaCommand("xz", []string{"-dc"}, src, dst, strip)
}

// extractTarZst extracts a .tar.zst archive using the system zstd
// command. If zstd is not available, returns an error.
func extractTarZst(src, dst string, strip int) error {
	// Try native Go zstd first
	if dec, err := newZstdReader(src); err == nil {
		defer dec.Close()
		return extractTarFromReader(dec, dst, strip)
	}
	// Fallback: use system zstd command
	return extractViaCommand("zstd", []string{"-dc"}, src, dst, strip)
}

// extract7z extracts a .7z archive using the system 7z command.
//
// Security (C-6): Unlike the built-in zip/tar extractors, the 7z command
// does not enforce path containment. To prevent zip-slip attacks where
// files escape the destination directory, we first list the archive
// contents and validate all paths BEFORE extracting. This prevents
// escaped files from ever being written to disk.
func extract7z(src, dst string) error {
	// Try 7z command first, then 7za
	for _, cmd := range []string{"7z", "7za"} {
		if _, err := exec.LookPath(cmd); err == nil {
			// Security (C-6): Pre-validate archive contents before extraction.
			// List the archive and check that every entry path stays within dst.
			if err := validate7zArchive(cmd, src, dst); err != nil {
				return fmt.Errorf("7z archive contains unsafe paths: %w", err)
			}

			// Now extract — all paths have been pre-validated
			c := exec.Command(cmd, "x", src, "-o"+dst, "-y")
			// Ticket 38 (P2-26): hide the console window (no-op off Windows).
			util.HideWindow(c)
			c.Stdout = os.Stderr
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return err
			}
			// Post-extraction validation as defense-in-depth
			if err := validateExtraction(dst); err != nil {
				if rmErr := os.RemoveAll(dst); rmErr != nil {
					log.Printf("[extractor] WARNING: failed to clean up after validation failure: %v", rmErr)
				}
				return fmt.Errorf("7z extraction validation failed (cleaned up): %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("7z extraction requires 7z or 7za command to be installed")
}

// validate7zArchive lists the contents of a 7z archive and validates
// that every file path would be extracted within the destination directory.
// This prevents zip-slip attacks where 7z would write files outside dst.
func validate7zArchive(cmd, src, dst string) error {
	// Use "7z l -slt" to list archive contents in a machine-readable format.
	// Each entry is separated by blank lines, with "Path = <value>" lines.
	c := exec.Command(cmd, "l", "-slt", src)
	// Ticket 38 (P2-26): hide the console window (no-op off Windows).
	util.HideWindow(c)
	output, err := c.Output()
	if err != nil {
		return fmt.Errorf("list archive contents: %w", err)
	}

	absDst, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for "Path = <value>" lines in the 7z listing output
		if !strings.HasPrefix(line, "Path = ") {
			continue
		}
		entryPath := strings.TrimPrefix(line, "Path = ")
		// Skip the archive itself (7z lists the archive as the first entry)
		if entryPath == "" || entryPath == src {
			continue
		}
		// Check that this entry would land within dst
		resolved := filepath.Join(absDst, entryPath)
		if !isSafePath(absDst, resolved) {
			return fmt.Errorf("entry %q escapes destination directory", entryPath)
		}
	}
	return nil
}

// validateExtraction walks dst and verifies that no symlinks, hard links,
// or special files escape the destination directory. This is a post-hoc
// check for extractors (like 7z) that don't provide per-file path validation.
func validateExtraction(dst string) error {
	_, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return fmt.Errorf("validate extraction: %w", err)
	}
	return filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := info.Mode()
		// Reject special file types that could be used for exploitation
		if mode&os.ModeDevice != 0 {
			return fmt.Errorf("unsafe device file: %s", path)
		}
		if mode&os.ModeNamedPipe != 0 {
			return fmt.Errorf("unsafe named pipe: %s", path)
		}
		if mode&os.ModeSocket != 0 {
			return fmt.Errorf("unsafe socket: %s", path)
		}
		// Check for hard links (nlink > 1 on regular files)
		if mode.IsRegular() && info.Sys() != nil {
			if nlink := getNlink(info); nlink > 1 {
				return fmt.Errorf("unsafe hard link (nlink=%d): %s", nlink, path)
			}
		}
		// Check for symlinks that might escape dst
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return nil // skip unreadable symlinks
			}
			if filepath.IsAbs(target) {
				return fmt.Errorf("unsafe symlink: %s -> absolute target %s", path, target)
			}
			resolved := filepath.Join(filepath.Dir(path), target)
			if !isSafePath(dst, resolved) {
				return fmt.Errorf("unsafe symlink: %s -> target %s escapes destination", path, target)
			}
		}
		return nil
	})
}

// extractViaCommand uses an external command to decompress, then
// pipes the output through the tar extractor.
func extractViaCommand(cmdName string, args []string, src, dst string, strip int) error {
	cmdArgs := append(args, src)
	cmd := exec.Command(cmdName, cmdArgs...)
	// Ticket 38 (P2-26): hide the console window (no-op off Windows).
	util.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("%s pipe: %w", cmdName, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s start: %w", cmdName, err)
	}
	err = extractTarFromReader(stdout, dst, strip)
	stdout.Close()
	if waitErr := cmd.Wait(); waitErr != nil {
		if err == nil {
			return fmt.Errorf("%s wait: %w", cmdName, waitErr)
		}
	}
	return err
}

// extractTarFromReader reads tar data from r and extracts to dst.
//
// Security (H-1): Before writing each entry, we check that no parent
// directory of the output path is a symlink. This prevents cross-entry
// symlink traversal where a previous entry creates a symlink that
// causes a later entry to write outside dst.
func extractTarFromReader(r io.Reader, dst string, strip int) error {
	tr := tar.NewReader(r)
	var totalExtracted int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		rel, ok := stripPath(hdr.Name, strip)
		if !ok {
			continue
		}
		// Ticket 38 (P2-02): same rel-level absolute/UNC rejection as the zip
		// path (see extractZip).
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, `\\`) {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("tar slip: entry %q has an absolute/UNC name", hdr.Name)
		}
		out := filepath.Join(dst, rel)
		if !isSafePath(dst, out) {
			_ = os.RemoveAll(dst)
			return fmt.Errorf("tar slip: path %q escapes destination %q", hdr.Name, dst)
		}
		// Security (H-1): Check that no ancestor of out is a symlink.
		// A previous tar entry could have created a symlink at a parent
		// directory, causing this write to land outside dst even though
		// the lexical path appears safe.
		if err := checkNoSymlinkAncestor(dst, out); err != nil {
			return fmt.Errorf("tar slip: cannot write %q: %w", hdr.Name, err)
		}
		mode := hdr.FileInfo().Mode()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(util.ToLongPath(out), mode.Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(util.ToLongPath(filepath.Dir(out)), 0o755); err != nil {
				return err
			}
			w, err := os.OpenFile(util.ToLongPath(out), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
			if err != nil {
				return err
			}
			n, err := io.Copy(w, io.LimitReader(tr, maxDecompressSize+1))
			if err != nil {
				w.Close()
				return err
			}
			if n > maxDecompressSize {
				w.Close()
				return fmt.Errorf("file %s exceeds max decompression size (%d bytes)", hdr.Name, maxDecompressSize)
			}
			w.Close()
			totalExtracted += n
			// Security (M-11): Check total extraction size
			if totalExtracted > maxTotalExtractSize {
				_ = os.RemoveAll(dst)
				return fmt.Errorf("total extraction size exceeds %d bytes limit (zip bomb protection)", maxTotalExtractSize)
			}
		case tar.TypeSymlink:
			// Validate symlink target stays within dst
			linkTarget := hdr.Linkname
			if filepath.IsAbs(linkTarget) {
				return fmt.Errorf("tar slip: symlink %q has absolute target %q", hdr.Name, linkTarget)
			}
			resolvedTarget := filepath.Join(filepath.Dir(out), linkTarget)
			if !isSafePath(dst, resolvedTarget) {
				return fmt.Errorf("tar slip: symlink %q target %q escapes destination %q", hdr.Name, linkTarget, dst)
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			_ = os.Remove(out)
			if err := os.Symlink(linkTarget, out); err != nil {
				return err
			}
		case tar.TypeLink:
			// Ticket 38 (P2-01): hard links had NO case here, so they were
			// silently dropped — archives that deduplicate files with links
			// (common in Go/Python distributions) came out incomplete.
			//
			// hdr.Linkname is an ARCHIVE path (relative to the archive root)
			// and must be stripped exactly like hdr.Name — NOT resolved
			// against the entry's directory the way a symlink target is
			// (that is the POSIX distinction between the two types).
			linkRel, linkOK := stripPath(hdr.Linkname, strip)
			if !linkOK {
				log.Printf("[extractor] WARNING: hard link %q target %q lies outside the stripped prefix — skipping", hdr.Name, hdr.Linkname)
				continue
			}
			linkTargetPath := filepath.Join(dst, linkRel)
			if !isSafePath(dst, linkTargetPath) {
				return fmt.Errorf("tar slip: hard link %q target %q escapes destination %q", hdr.Name, hdr.Linkname, dst)
			}
			// tar writes members in order, so the target normally exists
			// already; a missing target is skipped with a warning rather
			// than failing the whole extraction.
			if _, statErr := os.Stat(util.ToLongPath(linkTargetPath)); statErr != nil {
				log.Printf("[extractor] WARNING: hard link %q target %q not present — skipping link", hdr.Name, linkRel)
				continue
			}
			if err := os.MkdirAll(util.ToLongPath(filepath.Dir(out)), 0o755); err != nil {
				return err
			}
			_ = os.Remove(util.ToLongPath(out))
			if linkErr := os.Link(util.ToLongPath(linkTargetPath), util.ToLongPath(out)); linkErr != nil {
				// Cross-device links, filesystems without link support or
				// Windows ACLs can all refuse — copy instead of losing data.
				log.Printf("[extractor] WARNING: hard link %q -> %q failed (%v) — copying instead", hdr.Name, linkRel, linkErr)
				if copyErr := util.CopyFile(util.ToLongPath(linkTargetPath), util.ToLongPath(out)); copyErr != nil {
					return fmt.Errorf("hard link %q: link failed (%v) and copy fallback failed: %w", hdr.Name, linkErr, copyErr)
				}
			}
		}
	}
	return nil
}

// checkNoSymlinkAncestor verifies that no component of the output path
// between dst and out is a symlink. This prevents cross-entry symlink
// traversal attacks (H-1) where a previous tar entry creates a symlink
// that causes a later regular-file entry to write outside dst.
//
// For example, if entry 1 creates a symlink "subdir -> /tmp", then
// entry 2 writing "subdir/payload" would land in /tmp/payload even
// though the lexical path "subdir/payload" appears safe.
func checkNoSymlinkAncestor(dst, out string) error {
	absDst, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(filepath.Clean(out))
	if err != nil {
		return err
	}

	// Walk from dst downward, checking each component for symlinks.
	// We stop when we reach the output path.
	rel, err := filepath.Rel(absDst, absOut)
	if err != nil {
		return err
	}
	// If the output is the same as dst, nothing to check
	if rel == "." {
		return nil
	}

	current := absDst
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		fi, err := os.Lstat(current)
		if err != nil {
			// Path doesn't exist yet — can't be a symlink
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent directory %q is a symlink — possible traversal attack", current)
		}
	}
	return nil
}

// isSafePath checks that the resolved output path stays within the
// destination directory.  This prevents Zip Slip / Tar Slip attacks
// where a malicious archive contains paths like "../../etc/passwd".
// On Windows, the comparison is case-insensitive to match the filesystem.
func isSafePath(dst, out string) bool {
	absDst, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return false
	}
	absOut, err := filepath.Abs(filepath.Clean(out))
	if err != nil {
		return false
	}
	// Security (M-8): Reject Windows special paths
	if runtime.GOOS == "windows" {
		// Reject UNC paths (\\?\, \\.\, \\server\share) — but only when the
		// DESTINATION is not itself UNC: with an UNC install root every
		// entry would otherwise be rejected as "zip slip" (ticket 38 /
		// P2-02). Archive-relative names starting with \\ are still blocked
		// by the rel-level check in the extractors.
		if strings.HasPrefix(absOut, `\\`) && !strings.HasPrefix(absDst, `\\`) {
			return false
		}
		// Reject cross-volume paths
		if filepath.VolumeName(absOut) != filepath.VolumeName(absDst) {
			return false
		}
		// Reject Alternate Data Streams (colon after drive letter)
		volLen := len(filepath.VolumeName(absOut))
		if volLen > 0 && strings.Contains(absOut[volLen:], ":") {
			return false
		}
		// Reject Windows reserved device names
		base := filepath.Base(absOut)
		upperBase := strings.ToUpper(base)
		if dotIdx := strings.Index(upperBase, "."); dotIdx > 0 {
			upperBase = upperBase[:dotIdx]
		}
		for _, reserved := range []string{"CON", "PRN", "AUX", "NUL",
			"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"} {
			if upperBase == reserved {
				return false
			}
		}
		// Reject trailing dots or spaces
		trimmed := strings.TrimRight(base, ". ")
		if trimmed != base && trimmed != "" {
			return false
		}
	}
	// Also check via filepath.Rel that the path does not escape.
	// This catches cases where Clean resolves .. but the relative
	// path still escapes (e.g. on case-insensitive filesystems).
	if rel, err := filepath.Rel(absDst, absOut); err == nil {
		if strings.HasPrefix(rel, "..") {
			return false
		}
	}
	// Ensure the output path is under the destination directory.
	// We add a separator to prevent prefix matching issues
	// (e.g. /tmp/foo matching /tmp/foobar).
	// On Windows, paths are case-insensitive.
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absOut, absDst) ||
			strings.HasPrefix(strings.ToLower(absOut), strings.ToLower(absDst)+string(os.PathSeparator))
	}
	return strings.HasPrefix(absOut, absDst+string(os.PathSeparator)) || absOut == absDst
}

// getNlink returns the number of hard links for a file. On Windows it
// always returns 1 (hard link detection is unreliable). On Unix it
// reads the link count from the underlying syscall.Stat_t.
func getNlink(info os.FileInfo) uint64 {
	if runtime.GOOS == "windows" {
		return 1 // Windows doesn't reliably expose nlink
	}
	return getNlinkUnix(info)
}

// stripPath discards the first `strip` components of p.
//
// Returns (relativePath, true) when the path is non-empty after
// stripping, otherwise ("", false) meaning the entry should be
// skipped.
func stripPath(p string, strip int) (string, bool) {
	// F-20: clamp a negative strip. The value is manifest-controlled; the
	// pre-fix code only guarded the upper bound, so parts[-1:] panicked.
	if strip < 0 {
		strip = 0
	}
	p = filepath.Clean(p)
	if p == "." || p == "/" {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	if strip >= len(parts) {
		return "", false
	}
	rel := strings.Join(parts[strip:], "/")
	if rel == "" {
		return "", false
	}
	return rel, true
}

// gzipReaderAdapter bridges the *gzip.Reader return type to the
// io.ReadCloser signature our extractTar signature uses.
func gzipReaderAdapter(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

// bzip2ReaderAdapter wraps bzip2.NewReader to return io.ReadCloser.
func bzip2ReaderAdapter(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(bzip2.NewReader(r)), nil
}

// ---- Native xz/zstd support (optional, via build tags) ----
// These are implemented in separate files with build tags:
//   - xz_go.go    - pure Go xz decoder (uses github.com/xi2/xz)
//   - xz_fallback.go - stub when xz package is not available
//   - zstd_go.go  - pure Go zstd decoder (uses github.com/klauspost/compress/zstd)
//   - zstd_fallback.go - stub when zstd package is not available
