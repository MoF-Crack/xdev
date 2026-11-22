package extractor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Budget applies to one extraction, across all members and all bundles.
// The caller (not the archive) selects larger limits for trusted large tools.
type Budget struct {
	MaxFileBytes, MaxTotalBytes int64
	MaxEntries, MaxDepth        int
	Timeout                     time.Duration
	ReserveBytes                uint64 // free-space headroom, not an OS quota against concurrent writers
}

func DefaultBudget() Budget {
	return Budget{1 << 30, 10 << 30, 200000, 128, 30 * time.Minute, 64 << 20}
}

type extraction struct {
	ctx context.Context
	Budget
	dst       string
	total     int64
	entries   int
	buf       []byte
	spaceLeft uint64
}

// ExtractWithOptions writes only into a fresh private sibling until every
// decoder, write, close and validation has succeeded. Never merge/replace a
// populated installation. The installer must serialize publishers to dst.
func ExtractWithOptions(ctx context.Context, src, dst, format string, strip int, b Budget) (err error) {
	if strip < 0 {
		return fmt.Errorf("invalid strip value %d: must be >= 0", strip)
	}
	if b.MaxFileBytes <= 0 || b.MaxTotalBytes <= 0 || b.MaxEntries <= 0 || b.MaxDepth <= 0 || b.Timeout <= 0 {
		return fmt.Errorf("invalid extraction budget")
	}
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	parent := filepath.Dir(dst)
	if parent == dst {
		return fmt.Errorf("refusing extraction into filesystem root")
	}
	if err = os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	empty := func() (bool, error) {
		fi, e := os.Lstat(dst)
		if os.IsNotExist(e) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		if !fi.IsDir() {
			return false, fmt.Errorf("destination not a directory: %s", dst)
		}
		f, e := os.Open(dst)
		if e != nil {
			return false, e
		}
		defer f.Close()
		_, e = f.Readdirnames(1)
		if e == io.EOF {
			return true, nil
		}
		if e == nil {
			return false, fmt.Errorf("destination is not empty: %s", dst)
		}
		return false, e
	}
	if _, err = empty(); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".xdev-extract-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(stage))
		}
	}()
	x := extraction{ctx: ctx, Budget: b, dst: stage, buf: make([]byte, 32<<10)}
	if err = x.unpack(src, format, strip); err != nil {
		return err
	}
	if err = x.validateLinks(); err != nil {
		return err
	}
	if !strings.EqualFold(format, "dmg") {
		if err = ensureExecutableBits(ctx, stage); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	exists, err := empty()
	if err != nil {
		return err
	}
	if exists {
		if err = os.Remove(dst); err != nil {
			return err
		}
	}
	if err = publishStage(ctx, stage, dst); err != nil && exists {
		err = errors.Join(err, os.Mkdir(dst, 0755))
	}
	return err
}

// A freshly closed directory can remain briefly open in Windows scanners.
// Retry transient sharing/access errors only, while dst is still absent.
func publishStage(ctx context.Context, stage, dst string) error {
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := os.Rename(stage, dst)
		if err == nil || i >= 6 || !retryPublish(err) {
			return err
		}
		if _, e := os.Lstat(dst); !os.IsNotExist(e) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After((5 * time.Millisecond) << i):
		}
	}
}

func (x *extraction) entry(name string, strip int) (string, error) {
	if err := x.ctx.Err(); err != nil {
		return "", err
	}
	if x.entries >= x.MaxEntries {
		return "", fmt.Errorf("archive entry count exceeds budget %d", x.MaxEntries)
	}
	x.entries++
	if !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 || strings.ContainsAny(name, `\:`) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive slip: unsafe raw entry %q", name)
	}
	depth := 0
	for _, c := range strings.Split(name, "/") {
		if c == ".." {
			return "", fmt.Errorf("archive slip: parent component in %q", name)
		}
		if c != "" && c != "." {
			depth++
		}
	}
	if depth > x.MaxDepth {
		return "", fmt.Errorf("archive depth exceeds budget: %q", name)
	}
	if bad := badComponent(name); bad != "" {
		return "", fmt.Errorf("archive slip: entry %q contains invalid component %q", name, bad)
	}
	rel, ok := stripPath(name, strip)
	if !ok {
		return "", nil
	}
	out := filepath.Join(x.dst, rel)
	if !isSafePath(x.dst, out) {
		return "", fmt.Errorf("archive slip: %q escapes destination", name)
	}
	return out, checkNoSymlinkAncestor(x.dst, out)
}
func (x *extraction) checkSize(n int64) error {
	if n < 0 || n > x.MaxFileBytes || n > x.MaxTotalBytes-x.total {
		return fmt.Errorf("archive expanded byte budget exceeded (entry %d, remaining %d)", n, x.MaxTotalBytes-x.total)
	}
	return nil
}
func (x *extraction) space(n uint64) error {
	if x.ReserveBytes == 0 {
		return nil
	}
	if n > x.spaceLeft || x.spaceLeft == 0 {
		free, err := availableBytes(x.dst)
		if err != nil {
			return err
		}
		if free < x.ReserveBytes || free-x.ReserveBytes < n {
			return fmt.Errorf("archive free-space reserve would be exceeded")
		}
		x.spaceLeft = min(free-x.ReserveBytes, 8<<20)
	}
	return nil
}

// Never hand the filesystem writer a chunk larger than the remaining logical
// quota. Probe at EOF in memory, so the byte beyond the limit is never written.
func (x *extraction) file(out string, mode os.FileMode, size int64, r io.Reader) (err error) {
	if err = x.checkSize(size); err != nil {
		return err
	}
	if err = x.space(uint64(size)); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	w, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, w.Close()) }()
	var done int64
	for {
		if err = x.ctx.Err(); err != nil {
			return err
		}
		n, re := r.Read(x.buf)
		if n > 0 {
			nn := int64(n)
			if nn > size-done || nn > x.MaxFileBytes-done || nn > x.MaxTotalBytes-x.total {
				return fmt.Errorf("archive stream exceeds declared size or remaining budget: %s", filepath.Base(out))
			}
			if err = x.space(uint64(n)); err != nil {
				return err
			}
			written, we := w.Write(x.buf[:n])
			done += int64(written)
			x.total += int64(written)
			x.spaceLeft -= min(x.spaceLeft, uint64(written))
			if we != nil {
				return we
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if done != size {
		return fmt.Errorf("archive member size mismatch: got %d want %d", done, size)
	}
	return nil
}
