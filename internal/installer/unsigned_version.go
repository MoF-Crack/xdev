package installer

import "github.com/xmdemt/xdev/internal/manifest"

// DeriveUnsignedVersion reports whether (tool manifest, version) is
// legitimately absent from the signed SHA-256 map — audit-fixes ticket 13
// (P0-02 phase 1 / A-02).
//
// It is intentionally computed WITHOUT any hash condition:
//
//   - v.Discovered || v.Unverified → sniff-discovered / unverified version;
//   - version not found in manifest → custom tool or unknown version;
//   - nil manifest (Lua/vfox plugin) → unsigned by construction.
//
// Adding a "hash is empty" term would flip the verdict after the TOFU
// write-back (the hash becomes real) and make re-installing the same
// version fail with a false "hash tampering" verdict — that is exactly the
// regression this helper exists to prevent (guard G6).
//
// Both install pipelines (CLI installer and GUI) MUST call this single
// implementation: deriving it separately is how the two pipelines drift
// apart (the GUI's older versionUnverified helper only looked at
// Unverified and carried an npm special case).
func DeriveUnsignedVersion(m *manifest.Tool, ver string) bool {
	if m == nil {
		return true
	}
	if v, found := m.FindVersion(ver); found {
		return v.Discovered || v.Unverified
	}
	return true
}
