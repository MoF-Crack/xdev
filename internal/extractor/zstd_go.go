package extractor

import (
	"os"

	"github.com/klauspost/compress/zstd"
)

// newZstdReader opens the file at src and returns a zstd-decoded
// io.ReadCloser using the pure Go github.com/klauspost/compress/zstd library.
func newZstdReader(src string) (readCloser, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	r, err := zstd.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &zstdReadCloser{f: f, r: r}, nil
}

// zstdReadCloser wraps the file handle and zstd decoder so that both
// are closed properly.
type zstdReadCloser struct {
	f *os.File
	r *zstd.Decoder
}

func (z *zstdReadCloser) Read(p []byte) (int, error) { return z.r.Read(p) }
func (z *zstdReadCloser) Close() error {
	z.r.Close()
	return z.f.Close()
}
