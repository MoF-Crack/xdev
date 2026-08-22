// Package version holds the single source of truth for the
// xdev version string.  All other packages should reference
// this constant instead of hard-coding their own copy.
package version

const Version = "0.3.3"

// BuildDate is set via -ldflags at build time.
var BuildDate = ""

// GitCommit is set via -ldflags at build time (Spec 004: provenance).
var GitCommit = ""

// Get returns the version string.
func Get() string {
	return Version
}
