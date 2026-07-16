package tui

import (
	"strings"

	"github.com/xmdemt/xdev/internal/plugin"
)

// fuzzyMatch performs simple fuzzy matching: each character in the query
// must appear in the target string in order, but not necessarily contiguously.
// Returns true if query fuzzy-matches target.
func fuzzyMatch(query, target string) bool {
	q := strings.ToLower(query)
	t := strings.ToLower(target)

	qi := 0
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if q[qi] == t[ti] {
			qi++
		}
	}
	return qi == len(q)
}

// fuzzyFilter returns plugins whose name, display name, or description
// fuzzy-matches the query string.
func fuzzyFilter(tools []plugin.Plugin, query string) []plugin.Plugin {
	if query == "" {
		return tools
	}
	var result []plugin.Plugin
	for _, t := range tools {
		if fuzzyMatch(query, t.Name()) || fuzzyMatch(query, t.DisplayName()) || fuzzyMatch(query, t.Description()) {
			result = append(result, t)
		}
	}
	return result
}
