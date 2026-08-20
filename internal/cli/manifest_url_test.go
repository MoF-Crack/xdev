package cli

import "testing"

func TestValidateGitCloneURL_RejectsInsecureAndHelpers(t *testing.T) {
	denied := []string{
		"http://example.com/repo.git",
		"git://example.com/repo.git",
		"ext::sh -c evil",
		"ssh://git@example.com/repo.git",
		"file:///tmp/repo",
		"ftps://example.com/repo.git",
	}
	for _, raw := range denied {
		if err := validateGitCloneURL(raw); err == nil {
			t.Errorf("validateGitCloneURL(%q) should be rejected", raw)
		}
	}
}
