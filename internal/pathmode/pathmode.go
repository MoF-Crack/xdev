// Package pathmode defines PATH routing without depending on configuration or I/O.
package pathmode

const (
	Shim   = "shim"
	Direct = "direct"
	Auto   = "auto"
)

func Valid(mode string) bool {
	return mode == Shim || mode == Direct || mode == Auto
}

// Normalize preserves valid modes and falls back to shim for legacy values.
func Normalize(mode string) string {
	if Valid(mode) {
		return mode
	}
	return Shim
}

// UsesShims reports whether tool dispatch goes through the configured shim dir.
func UsesShims(mode string) bool { return Normalize(mode) != Direct }
