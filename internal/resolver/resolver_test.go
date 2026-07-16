package resolver

import "testing"

func TestParseLatest(t *testing.T) {
	spec, err := Parse("latest")
	if err != nil {
		t.Fatalf("Parse(latest) error: %v", err)
	}
	if spec.Kind != KindLatest {
		t.Errorf("expected KindLatest, got %d", spec.Kind)
	}
}

func TestParseLTS(t *testing.T) {
	spec, err := Parse("lts")
	if err != nil {
		t.Fatalf("Parse(lts) error: %v", err)
	}
	if spec.Kind != KindLTS {
		t.Errorf("expected KindLTS, got %d", spec.Kind)
	}
}

func TestParseExact(t *testing.T) {
	spec, err := Parse("3.12.7")
	if err != nil {
		t.Fatalf("Parse(3.12.7) error: %v", err)
	}
	if spec.Kind != KindExact {
		t.Errorf("expected KindExact, got %d", spec.Kind)
	}
}

func TestParseRange(t *testing.T) {
	spec, err := Parse("^3.12")
	if err != nil {
		t.Fatalf("Parse(^3.12) error: %v", err)
	}
	if spec.Kind != KindRange {
		t.Errorf("expected KindRange, got %d", spec.Kind)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Errorf("expected error for empty spec")
	}
}

func TestParseInvalid(t *testing.T) {
	_, err := Parse("not-a-version!!!")
	if err == nil {
		t.Errorf("expected error for invalid spec")
	}
}

func TestResolveExact(t *testing.T) {
	avail := []string{"3.10.0", "3.11.0", "3.12.0"}
	spec, _ := Parse("3.11.0")
	got, err := Resolve(spec, avail, "3.12.0", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if got != "3.11.0" {
		t.Errorf("expected 3.11.0, got %s", got)
	}
}

func TestResolveExactMissing(t *testing.T) {
	avail := []string{"3.10.0", "3.11.0"}
	spec, _ := Parse("3.99.0")
	_, err := Resolve(spec, avail, "", "")
	if err == nil {
		t.Errorf("expected error for missing version")
	}
}

func TestResolveLatest(t *testing.T) {
	avail := []string{"3.10.5", "3.12.7", "3.11.10"}
	spec, _ := Parse("latest")
	got, err := Resolve(spec, avail, "3.12.7", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if got != "3.12.7" {
		t.Errorf("expected 3.12.7 (manifest latest), got %s", got)
	}
}

func TestResolveLTSFallback(t *testing.T) {
	avail := []string{"3.10.5", "3.11.10", "3.12.7"}
	spec, _ := Parse("lts")
	// manifest declares no LTS, fallback to highest available
	got, err := Resolve(spec, avail, "", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if got != "3.12.7" {
		t.Errorf("expected 3.12.7, got %s", got)
	}
}

func TestResolveRangeCaret(t *testing.T) {
	avail := []string{"3.10.5", "3.11.10", "3.12.7"}
	spec, _ := Parse("^3.11")
	got, err := Resolve(spec, avail, "", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// ^3.11 in npm-semver means >=3.11.0 <4.0.0, so 3.12.7 is the
	// highest match (caret keeps the major fixed but allows minor
	// and patch to advance).
	if got != "3.12.7" {
		t.Errorf("expected 3.12.7, got %s", got)
	}
}

func TestResolveRangeTilde(t *testing.T) {
	avail := []string{"3.10.5", "3.11.10", "3.12.7"}
	spec, _ := Parse("~3.11")
	got, err := Resolve(spec, avail, "", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// ~3.11 means >=3.11.0 <3.12.0, so the only match is 3.11.10.
	if got != "3.11.10" {
		t.Errorf("expected 3.11.10, got %s", got)
	}
}

func TestResolveRangeNoMatch(t *testing.T) {
	avail := []string{"3.10.5", "3.11.10"}
	spec, _ := Parse("^4.0")
	_, err := Resolve(spec, avail, "", "")
	if err == nil {
		t.Errorf("expected error for no match")
	}
}
