package env

import (
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

func TestEnrichFromRegistry_MergesWhitelistOnly(t *testing.T) {
	if len(util.AllowedEnvVarNames) == 0 {
		t.Fatal("whitelist empty")
	}
	in := []string{"PATH=/bin", "UNRELATED=keep"}
	out := EnrichFromRegistry(in)
	if len(out) < 2 {
		t.Fatalf("expected original entries preserved, got %v", out)
	}
	var sawUnrelated bool
	for _, e := range out {
		if e == "UNRELATED=keep" {
			sawUnrelated = true
		}
		if strings.HasPrefix(e, "UNRELATED=") && e != "UNRELATED=keep" {
			t.Errorf("non-whitelist var mutated: %s", e)
		}
	}
	if !sawUnrelated {
		t.Error("UNRELATED should pass through")
	}
}
