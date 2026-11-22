package shim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

const dispatcherRecordsDir = ".xdev-dispatchers"

type dispatcherRecord struct {
	Schema int    `json:"schema"`
	Name   string `json:"name"`
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
}

func dispatcherDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("dispatcher source %s is not a regular file", path)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func dispatcherRecordPath(dir, name string) string {
	// Windows aliases share a case-insensitive namespace.
	if os.PathSeparator == '\\' {
		name = strings.ToLower(name)
	}
	key := sha256.Sum256([]byte(name))
	return filepath.Join(dir, dispatcherRecordsDir, hex.EncodeToString(key[:])+".json")
}

func dispatcherRecordDir(dir string, create bool) error {
	path := filepath.Join(dir, dispatcherRecordsDir)
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("dispatcher ownership directory %s is not a real directory", path)
	}
	return nil
}

func readDispatcherRecord(dir, name string) (dispatcherRecord, error) {
	var r dispatcherRecord
	if err := dispatcherRecordDir(dir, false); err != nil {
		return r, err
	}
	path := dispatcherRecordPath(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() {
		return r, fmt.Errorf("ownership record %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	equal := r.Name == name
	if os.PathSeparator == '\\' {
		equal = strings.EqualFold(r.Name, name)
	}
	digest, decodeErr := hex.DecodeString(r.SHA256)
	if r.Schema != 1 || !equal || util.ValidateShimName(r.Name) != nil || !filepath.IsAbs(r.Source) || decodeErr != nil || len(digest) != sha256.Size {
		return r, fmt.Errorf("invalid dispatcher ownership record for %s", name)
	}
	return r, nil
}

func writeDispatcherRecord(dir string, r dispatcherRecord) error {
	if err := dispatcherRecordDir(dir, true); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return util.AtomicWriteFile(dispatcherRecordPath(dir, r.Name), data, 0600)
}

func dispatcherOwned(dst string, r dispatcherRecord, selfPath string) (bool, error) {
	info, err := os.Lstat(dst)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(dst)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(dst), target)
		}
		if filepath.Clean(target) != filepath.Clean(r.Source) {
			return false, nil
		}
	} else if !info.Mode().IsRegular() {
		return false, nil
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false, err
	}
	// The source may have been atomically replaced at the same path. Existing
	// hardlinks/copies then retain the recorded digest; live symlinks share self.
	if srcInfo, err := os.Stat(selfPath); err == nil && os.SameFile(srcInfo, dstInfo) {
		return true, nil
	}
	digest, err := dispatcherDigest(dst)
	if err != nil {
		return false, err
	}
	return digest == r.SHA256, nil
}

func lockDispatchers(dir string) (*util.ToolLock, error) {
	return util.AcquireToolLock(dir, "dispatcher-management", 30*time.Second)
}

// replaceDispatcher stages the complete new entry before publishing. It never
// uses a fixed .old path, which might belong to a user or an earlier run.
func replaceDispatcher(selfPath, dst string) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".xdev-dispatcher-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	if err = f.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	defer os.Remove(temp)
	if err = os.Remove(temp); err != nil {
		return err
	}
	if os.PathSeparator == '\\' {
		err = placeWindowsShim(selfPath, temp)
	} else {
		err = os.Symlink(selfPath, temp)
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temp, dst); err == nil {
		return nil
	}
	if os.PathSeparator != '\\' {
		return err
	}
	// Windows can rename a running image although it cannot overwrite it. Keep
	// its displaced name unique, and restore it if publication itself fails.
	backup := temp + ".old"
	if moveErr := os.Rename(dst, backup); moveErr != nil {
		return fmt.Errorf("replace dispatcher: %w (displace: %v)", err, moveErr)
	}
	if publishErr := os.Rename(temp, dst); publishErr != nil {
		if restoreErr := os.Rename(backup, dst); restoreErr != nil {
			return fmt.Errorf("publish dispatcher: %w; restore displaced file: %v", publishErr, restoreErr)
		}
		return publishErr
	}
	_ = os.Remove(backup) // Running images may retain this uniquely named residue.
	return nil
}
