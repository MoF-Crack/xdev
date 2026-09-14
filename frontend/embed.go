package frontend

import "embed"

// DistFS is the embedded frontend distribution.
//
//go:embed all:dist
var DistFS embed.FS
