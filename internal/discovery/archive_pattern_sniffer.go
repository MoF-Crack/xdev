package discovery

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
)

// ---------------------------------------------------------------------------
// extractGitHubRepoFromURL
// ---------------------------------------------------------------------------

var githubDownloadRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/releases/download/`)

// extractGitHubRepoFromURL attempts to extract the GitHub owner and repo
// from a GitHub releases download URL of the form:
//
//	https://github.com/<owner>/<repo>/releases/download/<tag>/<filename>
//
// Returns (owner, repo, true) on success, ("", "", false) otherwise.
func extractGitHubRepoFromURL(url string) (owner, repo string, ok bool) {
	m := githubDownloadRe.FindStringSubmatch(url)
	if len(m) < 3 {
		return "", "", false
	}
	return m[1], m[2], true
}

// ---------------------------------------------------------------------------
// buildArchiveTemplate
// ---------------------------------------------------------------------------

// versionPlaceholder is the string used in archive URL templates that
// gets replaced with the actual version by FillArchivePlaceholders.
const versionPlaceholder = "{version}"

// buildArchiveTemplate takes an existing version and its archive entries
// and derives a URL template by replacing the concrete version number
// with the {version} placeholder.
//
// It returns the template archives and true on success.  If no archive
// can serve as template (e.g. empty archives list), it returns (nil, false).
func buildArchiveTemplate(existingVersion string, existingArchives []manifest.Archive) ([]manifest.Archive, bool) {
	if len(existingArchives) == 0 {
		return nil, false
	}

	templates := make([]manifest.Archive, len(existingArchives))
	for i, a := range existingArchives {
		t := a
		// Replace the existing version string in the URL with the
		// placeholder.  Prefer exact version occurrence to avoid
		// false positives in repo name or path.
		t.URL = strings.ReplaceAll(t.URL, existingVersion, versionPlaceholder)
		// Also replace in mirror URLs.
		t.MirrorURL = strings.ReplaceAll(t.MirrorURL, existingVersion, versionPlaceholder)
		for j := range t.MirrorURLs {
			t.MirrorURLs[j] = strings.ReplaceAll(t.MirrorURLs[j], existingVersion, versionPlaceholder)
		}
		// Zero out SHA-256 and size — they will be filled with
		// placeholders in FillArchivePlaceholders.
		t.SHA256 = ""
		t.Size = 0
		templates[i] = t
	}

	// Verify that at least one URL changed (otherwise the version
	// string does not appear in the URL, making the template useless).
	hasChange := false
	for _, t := range templates {
		if strings.Contains(t.URL, versionPlaceholder) {
			hasChange = true
			break
		}
	}
	if !hasChange {
		return nil, false
	}

	return templates, true
}

// ---------------------------------------------------------------------------
// archivePatternSniffer
// ---------------------------------------------------------------------------

// archivePatternSniffer discovers new versions for tools that have no
// explicit remote: configuration but whose existing archive URLs follow
// a GitHub releases download pattern (FR-004).
type archivePatternSniffer struct{}

func (s *archivePatternSniffer) Name() string { return "archive_pattern" }

// Supports returns true when the tool has no explicit remote (or
// remote type is "none"/"") AND at least one existing version has an
// archive URL from which a GitHub repo can be extracted.
func (s *archivePatternSniffer) Supports(t *manifest.Tool) bool {
	// Only activate when there is no explicit remote config.
	rt := strings.ToLower(t.Remote.Type)
	if rt == "github" || rt == "json" || rt == "html" {
		return false
	}

	// Look for any existing version with a GitHub download URL.
	for _, v := range t.VersionList {
		for _, a := range v.Archives {
			if _, _, ok := extractGitHubRepoFromURL(a.URL); ok {
				return true
			}
		}
	}
	return false
}

// Sniff derives a GitHub repo from the tool's existing archive URLs,
// discovers new tags via the GitHub Releases API, and generates
// DiscoveredVersion entries with archive templates (FR-004, FR-010).
//
// All returned versions have Unverified: true because the archive URLs
// are inferred, not from an explicit configuration.
func (s *archivePatternSniffer) Sniff(ctx context.Context, t *manifest.Tool) ([]DiscoveredVersion, error) {
	// Step 1: Find the first existing version with GitHub archives
	// and extract the repo info.
	var existingVer string
	var existingArchives []manifest.Archive
	var owner, repo string

	for _, v := range t.VersionList {
		for _, a := range v.Archives {
			if o, r, ok := extractGitHubRepoFromURL(a.URL); ok {
				existingVer = v.Version
				existingArchives = v.Archives
				owner, repo = o, r
				break
			}
		}
		if owner != "" {
			break
		}
	}

	if owner == "" {
		// No GitHub-derived repo found — silently unsupported.
		return nil, nil
	}

	// Step 2: Build archive template from existing archives.
	template, ok := buildArchiveTemplate(existingVer, existingArchives)
	if !ok {
		// Cannot derive a template — silently unsupported.
		return nil, nil
	}

	// Step 3: Discover new tags from GitHub.
	ghRemote := manifest.RemoteSource{
		Type:    "github",
		URL:     owner + "/" + repo,
		Pattern: t.Remote.Pattern, // inherit pattern if set
	}
	tags, err := remote.DiscoverVersions(ctx, ghRemote)
	if err != nil {
		return nil, fmt.Errorf("archive_pattern sniffer: discover %s/%s: %w", owner, repo, err)
	}

	// Step 4: Filter out versions already in the manifest.
	existingSet := make(map[string]bool, len(t.VersionList))
	for _, v := range t.VersionList {
		existingSet[v.Version] = true
	}

	// Step 5: Build DiscoveredVersion entries.
	var out []DiscoveredVersion
	for _, tag := range tags {
		if existingSet[tag] {
			continue
		}

		dv := DiscoveredVersion{
			Version:    tag,
			Source:     s.Name(),
			Unverified: true, // inferred source → unverified
		}

		// Fill archive placeholders from template.
		dv.Archives = fillArchivePlaceholdersFromTemplate(template, tag)

		out = append(out, dv)
	}

	if len(out) == 0 {
		log.Printf("[discovery] archive_pattern: %s/%s no new versions found (last known: %s)", owner, repo, existingVer)
	}

	return out, nil
}

// fillArchivePlaceholdersFromTemplate replaces the {version} placeholder
// in each archive URL with the concrete version, sets SHA-256 to zeros
// and Size to 0.  It is used internally by ArchivePatternSniffer and
// matches the semantics of FillArchivePlaceholders.
func fillArchivePlaceholdersFromTemplate(template []manifest.Archive, newVer string) []manifest.Archive {
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
