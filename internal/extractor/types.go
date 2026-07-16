package extractor

import "io"

// readCloser is an alias for io.ReadCloser, used by the xz/zstd
// decoder stubs and their real implementations.
type readCloser = io.ReadCloser
