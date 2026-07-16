package util

import "testing"

func TestCompareSemverEqual(t *testing.T) {
	if got := CompareSemver("1.2.3", "1.2.3"); got != 0 {
		t.Errorf("CompareSemver(1.2.3, 1.2.3) = %d, want 0", got)
	}
}

func TestCompareSemverGreater(t *testing.T) {
	if got := CompareSemver("2.0.0", "1.9.9"); got != 1 {
		t.Errorf("CompareSemver(2.0.0, 1.9.9) = %d, want 1", got)
	}
}

func TestCompareSemverLess(t *testing.T) {
	if got := CompareSemver("1.2.3", "1.2.4"); got != -1 {
		t.Errorf("CompareSemver(1.2.3, 1.2.4) = %d, want -1", got)
	}
}

func TestCompareSemverWithVPrefix(t *testing.T) {
	if got := CompareSemver("v1.0.0", "v1.0.1"); got != -1 {
		t.Errorf("CompareSemver(v1.0.0, v1.0.1) = %d, want -1", got)
		if got := CompareSemver("v1.0.1", "v1.0.0"); got != 1 {
			t.Errorf("CompareSemver(v1.0.1, v1.0.0) = %d, want 1", got)
		}
	}
}

func TestCompareSemverWithPreRelease(t *testing.T) {
	if got := CompareSemver("1.0.0-alpha", "1.0.0"); got != 0 {
		t.Errorf("CompareSemver(1.0.0-alpha, 1.0.0) = %d, want 0 (pre-release stripped)", got)
	}
}

func TestParseSemverPartsBasic(t *testing.T) {
	got := ParseSemverParts("1.2.3")
	want := [3]int{1, 2, 3}
	if got != want {
		t.Errorf("ParseSemverParts(1.2.3) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsZero(t *testing.T) {
	got := ParseSemverParts("0.1.0")
	want := [3]int{0, 1, 0}
	if got != want {
		t.Errorf("ParseSemverParts(0.1.0) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsVPrefix(t *testing.T) {
	got := ParseSemverParts("v1.0.0")
	want := [3]int{1, 0, 0}
	if got != want {
		t.Errorf("ParseSemverParts(v1.0.0) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsPreRelease(t *testing.T) {
	got := ParseSemverParts("2.3.4-beta.1")
	want := [3]int{2, 3, 4}
	if got != want {
		t.Errorf("ParseSemverParts(2.3.4-beta.1) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsLarge(t *testing.T) {
	got := ParseSemverParts("10.20.30")
	want := [3]int{10, 20, 30}
	if got != want {
		t.Errorf("ParseSemverParts(10.20.30) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsIncomplete(t *testing.T) {
	got := ParseSemverParts("1.2")
	want := [3]int{1, 2, 0}
	if got != want {
		t.Errorf("ParseSemverParts(1.2) = %v, want %v", got, want)
	}
}

func TestParseSemverPartsEmpty(t *testing.T) {
	got := ParseSemverParts("")
	want := [3]int{0, 0, 0}
	if got != want {
		t.Errorf("ParseSemverParts('') = %v, want %v", got, want)
	}
}
