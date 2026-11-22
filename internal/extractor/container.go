package extractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/xmdemt/xdev/internal/util"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Select by actual container. libarchive's @archive conversion re-emits
// length-framed metadata and data on stdout, never extracts to the filesystem.
// Do not use display-oriented '7z l -slt' as an authoritative name protocol.
func (x *extraction) container(src, format string, strip int) error {
	f, e := os.Open(src)
	if e != nil {
		return e
	}
	var h [512]byte
	n, re := io.ReadFull(f, h[:])
	defer f.Close()
	if re != nil && !errors.Is(re, io.ErrUnexpectedEOF) {
		return re
	}
	head := h[:n]
	if bytes.HasPrefix(head, []byte("PK\x03\x04")) || bytes.HasPrefix(head, []byte("PK\x05\x06")) {
		return x.zip(src, strip)
	}
	if n >= 262 && string(h[257:262]) == "ustar" {
		return x.unpack(src, "tar", strip)
	}
	// Do not let libarchive auto-detect unrelated formats such as mtree, whose
	// records can refer to files in the caller's local filesystem. Only expose
	// self-contained binary containers. For other installer payloads there is
	// no trustworthy raw-member protocol; never execute them or extract in place.
	if bytes.HasPrefix(head, []byte("7z\xbc\xaf\x27\x1c")) {
		return x.sevenZip(f, strip)
	}
	if !bytes.HasPrefix(head, []byte("xar!")) {
		return fmt.Errorf("unsupported %s container: expected ZIP, TAR, native 7z or XAR; refusing unstructured installer extraction", format)
	}
	abs, e := filepath.Abs(src)
	if e != nil {
		return e
	}
	bin, e := exec.LookPath("bsdtar")
	if e != nil && (runtime.GOOS == "windows" || runtime.GOOS == "darwin") {
		bin, e = exec.LookPath("tar")
	}
	if e != nil {
		return fmt.Errorf("%s requires a structured archive decoder (bsdtar/libarchive); refusing unsafe text listings", format)
	}
	// A real bsdtar may have a host-specific format set. A nonzero exit,
	// including an unsupported SFX/installer, invalidates the entire staging tree.
	ctx, cancel := context.WithCancel(x.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-Pcf", "-", "--format", "pax", "@"+abs)
	util.HideWindow(cmd)
	util.TrackProcessGroup(cmd)
	cmd.WaitDelay = 3 * time.Second
	rc, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	e = x.tar(rc, strip)
	if e == nil {
		_, e = io.Copy(io.Discard, cancelReader{ctx, rc})
	}
	if e != nil {
		cancel()
	}
	ce := rc.Close()
	we := cmd.Wait()
	if e = errors.Join(e, ce, we); e != nil {
		return fmt.Errorf("cannot safely stream %s with %s: %w", format, filepath.Base(bin), e)
	}
	return nil
}

// Prevent a chain of individually relative symlinks from resolving outside
// staging. os.Root resolves every hop inside the already-open directory.
func (x *extraction) validateLinks() error {
	root, e := os.OpenRoot(x.dst)
	if e != nil {
		return e
	}
	defer root.Close()
	return filepath.WalkDir(x.dst, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = x.ctx.Err(); e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, e := filepath.Rel(x.dst, p)
		if e != nil {
			return e
		}
		if _, e = root.Stat(rel); e != nil {
			return fmt.Errorf("invalid or unresolved archive link %q: %w", strings.TrimSpace(rel), e)
		}
		return nil
	})
}
