package extractor

import (
	"os"

	"github.com/xi2/xz"
)

// newXZReader opens the file at src and returns an xz-decoded
// io.ReadCloser using the pure Go github.com/xi2/xz library.
func newXZReader(src string) (readCloser, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	r, err := xz.NewReader(f, 0)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &xzReadCloser{f: f, r: r}, nil
}

// xzReadCloser wraps the file handle and xz reader so that both
// are closed properly.
type xzReadCloser struct {
	f *os.File
	r *xz.Reader
}

func (x *xzReadCloser) Read(p []byte) (int, error)  { return x.r.Read(p) }
func (x *xzReadCloser) Close() error                 { return x.f.Close() }
