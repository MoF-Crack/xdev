// Package buildassets provides the release icon to the native desktop runtime.
package buildassets

import _ "embed"

// AppIcon is the same F2 PNG used by native application packaging.
//
//go:embed appicon.png
var AppIcon []byte
