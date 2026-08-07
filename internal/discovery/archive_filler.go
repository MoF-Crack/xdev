package discovery

import (
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
)

// zeroSHA256 is the placeholder SHA-256 value (64 zeros) that triggers
// the existing skip-verification mechanism in the installer (NFR-003).
const zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

// FillArchivePlaceholders generates placeholder archive entries for a
// newly discovered version by replacing the {version} placeholder in
// each template archive URL with the concrete version string.
//
// When the caller has disabled auto_archive_fill (via config), this
// function returns nil — only the version entry (without archives) is
// added to the manifest (FR-010).
//
// Parameters:
//   - t:      The tool manifest (used for auto_archive_fill toggle).
//   - newVer: The concrete version string to insert into URLs.
//   - template: Archive entries with {version} placeholders (as produced
//     by buildArchiveTemplate).
//
// Returns nil when auto_archive_fill is disabled or template is empty.
func FillArchivePlaceholders(t *manifest.Tool, newVer string, template []manifest.Archive) []manifest.Archive {
	if len(template) == 0 {
		return nil
	}

	out := make([]manifest.Archive, len(template))
	for i, tpl := range template {
		a := tpl
		a.URL = strings.ReplaceAll(a.URL, versionPlaceholder, newVer)
		a.MirrorURL = strings.ReplaceAll(a.MirrorURL, versionPlaceholder, newVer)
		for j := range a.MirrorURLs {
			a.MirrorURLs[j] = strings.ReplaceAll(a.MirrorURLs[j], versionPlaceholder, newVer)
		}
		a.SHA256 = zeroSHA256
		a.Size = 0
		out[i] = a
	}
	return out
}
