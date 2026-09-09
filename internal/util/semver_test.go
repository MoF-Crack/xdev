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
	// Spec 039: SemVer 2.0.0 pre-release precedence — a release outranks
	// its own pre-releases (was: suffix stripped, compare 0).
	if got := CompareSemver("1.0.0-alpha", "1.0.0"); got != -1 {
		t.Errorf("CompareSemver(1.0.0-alpha, 1.0.0) = %d, want -1 (release > pre-release)", got)
	}
	if got := CompareSemver("1.0.0", "1.0.0-alpha"); got != 1 {
		t.Errorf("CompareSemver(1.0.0, 1.0.0-alpha) = %d, want 1", got)
	}
}

// TC-PR1 SemVer 2.0.0 预发布标识符比较（Spec 039，dsh rc/alpha 版本流）。
func TestCompareSemverPreReleasePrecedence(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// 数字标识符按数值比较：rc.2 > rc.1；alpha.10 > alpha.9（非字典序）。
		{"0.1.2-rc.2", "0.1.2-rc.1", 1},
		{"0.1.2-rc.1", "0.1.2-rc.2", -1},
		{"0.1.2-alpha.10", "0.1.2-alpha.9", 1},
		// 字母标识符按 ASCII：rc > alpha > beta? —— rc > alpha（字典序）。
		{"0.1.2-rc.1", "0.1.2-alpha.5", 1},
		{"0.1.2-alpha.5", "0.1.2-rc.1", -1},
		// 数字标识符低于字母标识符（SemVer 规则）。
		{"0.1.2-alpha", "0.1.2-1", 1},
		// 字段多者高：rc.1.2 > rc.1。
		{"0.1.2-rc.1.2", "0.1.2-rc.1", 1},
		// 同串相等。
		{"0.1.2-rc.1", "0.1.2-rc.1", 0},
		// 核心段优先：0.1.5-alpha.1 > 0.1.2-rc.1。
		{"0.1.5-alpha.1", "0.1.2-rc.1", 1},
		// build metadata 忽略。
		{"1.0.0-rc.1+build.5", "1.0.0-rc.1", 0},
		{"1.0.0+build.5", "1.0.0", 0},
		// v 前缀容忍。
		{"v0.1.2-rc.1", "0.1.2-rc.1", 0},
	}
	for _, tc := range cases {
		if got := CompareSemver(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TC-PR2 IsPrereleaseVersion 判定（Spec 039）。
func TestIsPrereleaseVersion(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"0.1.2-rc.1", true},
		{"0.1.5-alpha.1", true},
		{"v1.0.0-beta.2", true},
		{"1.0.0-rc.1+build", true},
		{"1.0.0", false},
		{"1.0.0+build", false},
		{"0.1.2", false},
		{"v2.0.0", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsPrereleaseVersion(tc.s); got != tc.want {
			t.Errorf("IsPrereleaseVersion(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

// TC-PR3 PrereleaseChannel 通道提取（Spec 039：alpha 通道不劫持 latest）。
func TestPrereleaseChannel(t *testing.T) {
	cases := []struct {
		s    string
		want string
	}{
		{"0.1.2-rc.1", "rc"},
		{"0.1.5-alpha.1", "alpha"},
		{"1.0.0-beta", "beta"},
		{"v2.0.0-RC.3", "RC"},
		{"1.0.0-rc.1.2", "rc"},
		{"1.0.0", ""},
		{"1.0.0+build", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := PrereleaseChannel(tc.s); got != tc.want {
			t.Errorf("PrereleaseChannel(%q) = %q, want %q", tc.s, got, tc.want)
		}
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
