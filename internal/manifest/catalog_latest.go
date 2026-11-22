package manifest

// Keep the ordinary-release compatibility helper on the same promotion policy
// as discovery, refresh and tool-aware catalog merging.
func canPromoteCatalogLatest(versions []Version, candidate, current string) bool {
	return CanPromoteToolLatest("", versions, candidate, current, true)
}
