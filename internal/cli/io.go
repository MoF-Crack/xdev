package cli

import (
	"io"
	"os"
)

// stderr returns the standard error stream.  Wrapping it in a
// function keeps the call sites short and makes it easy to redirect
// in tests.
func stderr() io.Writer { return os.Stderr }
