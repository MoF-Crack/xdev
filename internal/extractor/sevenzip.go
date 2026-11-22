package extractor

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"

	"github.com/bodgit/sevenzip"
)

type cancelReaderAt struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r cancelReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.ReadAt(p, off)
}

// Read a single, already-open container. Do not use OpenReader's automatic
// multipart sibling discovery or fs.FS's cleaned-name lookup. Opening each
// File object directly also avoids wildcard, charset and duplicate-name
// ambiguities of selecting a member on a command line.
func (x *extraction) sevenZip(f *os.File, strip int) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	r, err := sevenzip.NewReader(cancelReaderAt{x.ctx, f}, fi.Size())
	if err != nil {
		return err
	}
	if len(r.File) > x.MaxEntries {
		return fmt.Errorf("7z entry count exceeds budget %d", x.MaxEntries)
	}
	// Check the whole declared expansion (including members skipped by strip:
	// decoding a later solid-block member may require seeking through these).
	var predicted int64
	for _, m := range r.File {
		if err := x.ctx.Err(); err != nil {
			return err
		}
		if m.UncompressedSize > uint64(x.MaxFileBytes) || m.UncompressedSize > uint64(x.MaxTotalBytes-predicted) {
			return fmt.Errorf("7z declared expanded size exceeds budget at %q", m.Name)
		}
		predicted += int64(m.UncompressedSize)
	}
	for _, m := range r.File {
		out, err := x.entry(m.Name, strip)
		if err != nil {
			return fmt.Errorf("7z member: %w", err)
		}
		if out == "" {
			continue
		}
		mode := m.Mode()
		if m.Attributes&0x400 != 0 {
			return fmt.Errorf("unsupported 7z reparse point %q", m.Name)
		}
		if mode.IsDir() {
			if m.UncompressedSize != 0 {
				return fmt.Errorf("7z directory with data: %q", m.Name)
			}
			if err := os.MkdirAll(out, 0755); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			return fmt.Errorf("unsupported 7z member type %s: %q", mode.Type(), m.Name)
		}
		in, err := m.Open()
		if err != nil {
			return err
		}
		crc := crc32.NewIEEE()
		err = errors.Join(x.file(out, mode, int64(m.UncompressedSize), io.TeeReader(in, crc)), in.Close())
		if err != nil {
			return err
		}
		if m.CRC32 != 0 && crc.Sum32() != m.CRC32 {
			return fmt.Errorf("7z CRC mismatch for %q", m.Name)
		}
	}
	return nil
}
