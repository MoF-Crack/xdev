// Package resolver turns human-friendly version specifications such as
//
//	"latest"
//	"lts"
//	"3.12.0"
//	"^3.12"
//	"~3.11"
//
// into a concrete version that exists in the manifest.
//
// This implementation does not depend on a third-party semver library
// to keep the dependency surface small - we ship our own minimal
// semver parser/comparator.  It is intentionally not feature-complete
// (no pre-release tags, no build metadata) and is just enough to drive
// the rest of the tool.
package resolver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Spec is a structured representation of a user-supplied version spec.
type Spec struct {
	Raw        string
	Original   string
	Kind       Kind
	Constraint string // raw semver constraint when Kind == Range
}

// Kind enumerates the supported spec types.
type Kind int

const (
	KindExact Kind = iota
	KindLatest
	KindLTS
	KindRange
)

// Parse turns a string like "python@3.12" or "node@^20" into a Spec.
func Parse(raw string) (Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Spec{}, fmt.Errorf("empty version spec")
	}
	original := raw
	switch strings.ToLower(raw) {
	case "latest":
		return Spec{Raw: raw, Original: original, Kind: KindLatest}, nil
	case "lts":
		return Spec{Raw: raw, Original: original, Kind: KindLTS}, nil
	}
	// 第八轮审计 A-05（P1）：**先判 partial，再判 exact**。
	//
	// parseSemver 接受 2 段（`3.12`），因此 isExactSemver("3.12") 为真，旧顺序会把
	// `python@3.12` 归为 KindExact → 精确匹配 "3.12" 失败并报
	// "version 3.12 not found"。于是 isPartial / resolveRange 的 "x.y" 分支是
	// **完全死代码**，而 README.md 与 installation-guide.md 都把
	// `xdev install python@3.12` 当首要示例，resolver.go 的注释也承诺
	// `"3.12" -> any 3.12.x`。实测：`xdev install python@3.12` 报
	// "resolve version failed … version 3.12 not found"，而 `^3.12`/`~3.12`/`3.12.7`
	// 均能进入下载阶段。
	//
	// `3.12` 是「部分版本」（= 3.12.x 中最高者），必须走范围解析。
	if isPartial(raw) {
		return Spec{Raw: raw, Original: original, Kind: KindRange, Constraint: raw}, nil
	}
	if isExactSemver(raw) {
		return Spec{Raw: raw, Original: original, Kind: KindExact}, nil
	}
	if isConstraint(raw) {
		return Spec{Raw: raw, Original: original, Kind: KindRange, Constraint: raw}, nil
	}
	return Spec{}, fmt.Errorf("unrecognised version spec: %q", raw)
}

// Resolve picks the highest matching version from available.
func Resolve(spec Spec, available []string, latest, lts string) (string, error) {
	switch spec.Kind {
	case KindLatest:
		if latest != "" {
			return latest, nil
		}
		return highest(available), nil
	case KindLTS:
		if lts != "" {
			return lts, nil
		}
		return highest(available), nil
	case KindExact:
		// D-09（审计 P1-14，方案 B）：先原始串全等——内置清单与 available
		// 列表的既有形态（两侧都不带 v）走原路径，零回归；miss 后再做
		// **双侧** 去 "v" 前缀比较。原因是预填/自定义工具（remote.Pattern
		// 捕获组可能含 v）产出的版本可能带前缀（v3.12.0），而 available
		// 是不带前缀的 3.12.0，反之亦然——单侧去前缀只能修一半。
		//
		// 刻意不规范化 available 本身：返回值必须是列表里的**原始字符串**，
		// 调用方（installer/isolator）用它拼安装目录名；也保证自定义工具
		// 带 v 的产出仍可被匹配到。
		for _, v := range available {
			if v == spec.Raw {
				return v, nil
			}
		}
		want := strings.TrimPrefix(spec.Raw, "v")
		for _, v := range available {
			if strings.TrimPrefix(v, "v") == want {
				return v, nil
			}
		}
		return "", fmt.Errorf("version %s not found", spec.Raw)
	case KindRange:
		return resolveRange(spec.Constraint, available)
	}
	return "", fmt.Errorf("unsupported spec kind %d", spec.Kind)
}

// resolveRange picks the highest available version satisfying the
// given npm-style constraint.  We support:
//
//   - "3.12"      -> any 3.12.x
//   - "3.12.0"    -> exact (handled by KindExact normally, but allowed)
//   - "^3.12"     -> >= 3.12.0, < 4.0.0
//   - "~3.12"     -> >= 3.12.0, < 3.13.0
//   - ">=3.12"    -> >= 3.12.0
//   - "3.10 || 3.12"  -> OR of constraints
func resolveRange(constraint string, available []string) (string, error) {
	matched, err := filterVersions(constraint, available)
	if err != nil {
		return "", err
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no version matches %q", constraint)
	}
	return matched[len(matched)-1], nil // filterVersions returns ascending order
}

func filterVersions(constraint string, available []string) ([]string, error) {
	clauses := splitOrClauses(constraint)
	all := make([]string, 0, len(available))
	seen := map[string]bool{}
	for _, avail := range available {
		v, err := parseSemver(avail)
		if err != nil {
			continue
		}
		ok := false
		for _, c := range clauses {
			matched, err := matchClause(c, v)
			if err != nil {
				return nil, err
			}
			if matched {
				ok = true
				break
			}
		}
		if ok && !seen[avail] {
			all = append(all, avail)
			seen[avail] = true
		}
	}
	sortVersions(all)
	return all, nil
}

func splitOrClauses(c string) []string {
	parts := strings.Split(c, "||")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchClause returns whether v satisfies the npm-style clause.
func matchClause(clause string, v *semver) (bool, error) {
	clause = strings.TrimSpace(clause)
	if clause == "*" || clause == "" {
		return true, nil
	}
	// range operator
	if strings.HasPrefix(clause, ">=") {
		other, err := parseSemver(strings.TrimSpace(clause[2:]))
		if err != nil {
			return false, err
		}
		return v.compare(other) >= 0, nil
	}
	if strings.HasPrefix(clause, "<=") {
		other, err := parseSemver(strings.TrimSpace(clause[2:]))
		if err != nil {
			return false, err
		}
		return v.compare(other) <= 0, nil
	}
	if strings.HasPrefix(clause, ">") {
		other, err := parseSemver(strings.TrimSpace(clause[1:]))
		if err != nil {
			return false, err
		}
		return v.compare(other) > 0, nil
	}
	if strings.HasPrefix(clause, "<") {
		other, err := parseSemver(strings.TrimSpace(clause[1:]))
		if err != nil {
			return false, err
		}
		return v.compare(other) < 0, nil
	}
	// prefix operators
	if strings.HasPrefix(clause, "^") {
		base, err := parseSemver(strings.TrimSpace(clause[1:]))
		if err != nil {
			return false, err
		}
		upper := &semver{major: base.major + 1}
		return v.compare(base) >= 0 && v.compare(upper) < 0, nil
	}
	if strings.HasPrefix(clause, "~") {
		base, err := parseSemver(strings.TrimSpace(clause[1:]))
		if err != nil {
			return false, err
		}
		upper := &semver{major: base.major, minor: base.minor + 1}
		return v.compare(base) >= 0 && v.compare(upper) < 0, nil
	}
	// "x.y" => x.y.*
	if isPartial(clause) {
		parts := strings.Split(clause, ".")
		var major, minor int
		fmt.Sscanf(parts[0], "%d", &major)
		if len(parts) > 1 {
			fmt.Sscanf(parts[1], "%d", &minor)
		}
		return v.major == major && v.minor == minor, nil
	}
	// exact match
	other, err := parseSemver(clause)
	if err != nil {
		return false, err
	}
	return v.compare(other) == 0, nil
}

func isPartial(s string) bool {
	if strings.ContainsAny(s, "<>^~") {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	// partial means last is .x or 1- or 2-digit segments
	if len(parts) == 2 {
		return true
	}
	return false
}

func isExactSemver(s string) bool {
	_, err := parseSemver(s)
	return err == nil
}

func isConstraint(s string) bool {
	if isExactSemver(s) {
		return true
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "*" {
		return true
	}
	if strings.HasPrefix(trimmed, "^") || strings.HasPrefix(trimmed, "~") ||
		strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "<") {
		return true
	}
	return isPartial(trimmed)
}

// semver is a tiny semver representation.
// We store major/minor/patch and optional pre-release tag.
type semver struct {
	major, minor, patch int
	// extra 是第 4 段及以后的数值段（第九轮审计 P1-3）。
	//
	// 真实语料里存在 4 段构建号：clion/goland/webstorm 的 latest 是
	// "2026.2.0.1"，jetbrains-idea-community "2025.2.6.3"、
	// pycharm-community "2025.2.6.2"。旧实现用 SplitN(s, ".", 3)，于是第 3 段变成
	// "0.1"、Atoi 失败 → `unrecognised version spec`：显式安装报错，而范围安装会
	// 因 filterVersions 静默丢弃该条目而**永远选不到**（与 R8-2 同形态的
	// 「以为在更新，其实没推上去」）。
	//
	// 按「缺失段视为 0」比较，使 "2026.2" 与 "2026.2.0.0" 相等（与 util.CompareSemver
	// 对 3 段的既有语义一致），而 "2026.2.0.1" > "2026.2"。
	extra []int
	pre   string // e.g. "beta.1", "rc1"
}

func parseSemver(s string) (*semver, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// 第九轮审计 A-16（P2）：**先剥 build metadata，再切 pre-release**。
	//
	// 缺陷原形是反过来的两步：先按 '-' 切 pre-release，再按 '+' 去 build
	// metadata。对 "1.0.0-rc.1+build" 而言，第一步之后 '+' 已经落在
	// pre-release 段里（pre="rc.1+build"），第二步的 '+' 查找只作用于主版本
	// 段（此时已无 '+'）—— 于是 pre-release 残留 "+build"。实测
	// compare("1.0.0-rc.1+build","1.0.0-rc.1") = 1，而 SemVer 2.0.0 §10 要求
	// build metadata **不参与**优先级比较（应为 0）；同仓库的
	// util.CompareSemver 给出的正是 0，两者结论相反。
	//
	// 顺序调换后："1.0.0-rc.1+build" → 剥 '+' 得 "1.0.0-rc.1" → 切 '-' 得
	// pre="rc.1"。build metadata 里的 '-' 也不会再被误当成 pre-release 分隔符。
	if idx := strings.Index(s, "+"); idx >= 0 {
		s = s[:idx]
	}
	// Extract pre-release after '-'
	var pre string
	if idx := strings.Index(s, "-"); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	// 第九轮审计 P1-3：接受**任意段数**（≥1）。旧的 `SplitN(s, ".", 3)` 让第 3 段
	// 变成 "0.1"，4 段版本（真实语料 2026.2.0.1 等）直接解析失败。
	parts := strings.Split(s, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("not a semver: %q", s)
	}
	v := &semver{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("not a semver: %q", s)
		}
		switch i {
		case 0:
			v.major = n
		case 1:
			v.minor = n
		case 2:
			v.patch = n
		default:
			v.extra = append(v.extra, n)
		}
	}
	return v, nil
}

func (a *semver) compare(b *semver) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	// 第九轮审计 P1-3：第 4 段及以后按数值比较，**缺失段视为 0**
	// （于是 "2026.2" == "2026.2.0.0"，而 "2026.2.0.1" > "2026.2"）。
	// extra 属于「数值核心」，因此排在 pre-release 之前比较。
	if c := compareExtraSegments(a.extra, b.extra); c != 0 {
		return c
	}
	// Pre-release comparison: a version without pre-release is
	// higher than one with a pre-release tag.
	// e.g. 1.0.0 > 1.0.0-beta.1
	if a.pre == "" && b.pre != "" {
		return 1
	}
	if a.pre != "" && b.pre == "" {
		return -1
	}
	return comparePreRelease(a.pre, b.pre)
}

// compareExtraSegments 比较第 4 段及以后的数值段，缺失段视为 0。
// 长度不等时按最短长度补齐后比较（"1" vs "1.0" 相等，1 vs 1.0.1 → 后者更大）。
func compareExtraSegments(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// comparePreRelease compares two pre-release strings per SemVer 2.0.0.
// Returns -1, 0, or 1.
func comparePreRelease(a, b string) int {
	if a == b {
		return 0
	}
	// No pre-release has higher precedence than having pre-release
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		if i >= len(aParts) {
			return -1 // a has fewer fields, lower precedence
		}
		if i >= len(bParts) {
			return 1 // b has fewer fields, lower precedence
		}
		aIsNum := isNumeric(aParts[i])
		bIsNum := isNumeric(bParts[i])
		if aIsNum && bIsNum {
			ai, _ := strconv.Atoi(aParts[i])
			bi, _ := strconv.Atoi(bParts[i])
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		} else if aIsNum != bIsNum {
			// Numeric identifier have lower precedence than alphanumeric
			if aIsNum {
				return -1
			}
			return 1
		} else {
			// Both alphanumeric: lexical comparison
			if aParts[i] != bParts[i] {
				if aParts[i] < bParts[i] {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func sortVersions(in []string) {
	// Sort in place, preserving original strings (do NOT normalize
	// via semver.String() as that would break downstream lookups).
	type entry struct {
		original string
		parsed   *semver
	}
	entries := make([]entry, len(in))
	for i, v := range in {
		s, _ := parseSemver(v)
		entries[i] = entry{original: v, parsed: s}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].parsed == nil {
			return true
		}
		if entries[j].parsed == nil {
			return false
		}
		return entries[i].parsed.compare(entries[j].parsed) < 0
	})
	for i, e := range entries {
		in[i] = e.original
	}
}

func (v *semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

func highest(available []string) string {
	if len(available) == 0 {
		return ""
	}
	cp := append([]string{}, available...)
	sortVersions(cp)
	return cp[len(cp)-1]
}
