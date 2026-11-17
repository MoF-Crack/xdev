//go:build desktop && bindings

package frontend

import "embed"

// Static binding generation must not require or execute generated UI assets.
// This zero filesystem is never used by a distributed desktop build.
var DistFS embed.FS
