package hostsnapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/env"
)

func TestDiff_DetectsEnvChange(t *testing.T) {
	before := Snapshot{Env: map[string]string{"GOROOT": "A"}}
	after := Snapshot{Env: map[string]string{"GOROOT": "B"}}
	changes := Diff(before, after)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1: %#v", len(changes), changes)
	}
	if changes[0].Kind != "env" || changes[0].Key != "GOROOT" {
		t.Fatalf("unexpected change: %#v", changes[0])
	}
	if changes[0].Before != "A" || changes[0].After != "B" {
		t.Fatalf("unexpected values: %#v", changes[0])
	}
}

func TestDiff_NoChange(t *testing.T) {
	s := Snapshot{Env: map[string]string{"GOROOT": "X"}, Files: map[string]FileMeta{}}
	if got := Diff(s, s); len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}

func TestDiff_DetectsFileChange(t *testing.T) {
	before := Snapshot{Files: map[string]FileMeta{"/tmp/x": {Exists: false}}}
	after := Snapshot{Files: map[string]FileMeta{"/tmp/x": {Exists: true, Size: 1}}}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Fatalf("got %#v", changes)
	}
}

func TestDefaultOptions_IncludesKnownKeys(t *testing.T) {
	opts := DefaultOptions()
	found := false
	for _, k := range opts.EnvKeys {
		if k == "GOROOT" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("DefaultOptions missing GOROOT")
	}
	if len(opts.ExcludePathPrefixes) == 0 {
		t.Fatal("expected temp exclude prefixes")
	}
}

func TestCapture_RoundTripEnvKeys(t *testing.T) {
	opts := Options{EnvKeys: []string{"GOROOT", "GOPATH", "PATH"}}
	s, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Env["GOROOT"]; !ok {
		t.Fatal("missing GOROOT key in snapshot")
	}
	// Second capture should match for unchanged host.
	s2, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(s, s2); len(changes) != 0 {
		t.Fatalf("stable host should not change: %#v", changes)
	}
}

func TestCapture_FilePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "marker.txt")
	opts := Options{Paths: []string{p}}
	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if before.Files[filepath.Clean(p)].Exists {
		t.Fatal("expected missing before create")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Fatalf("want file change, got %#v", changes)
	}
}

func TestCapture_ExcludesTempPrefix(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ignored.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Paths:               []string{p},
		ExcludePathPrefixes: []string{dir},
	}
	s, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 0 {
		t.Fatalf("excluded path should not be captured: %#v", s.Files)
	}
}

// TestCapture_DetectsSameSizeRewrite 证明指纹含 mtime：等长改写（例如
// 真实 config.yaml 被测试覆盖成另一段等长内容）也必须被判为变化，
// 否则宿主污染审计会出现「改了但看不见」的假绿。
func TestCapture_DetectsSameSizeRewrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "same-size.txt")
	opts := Options{Paths: []string{p}}

	if err := os.WriteFile(p, []byte("AAAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := func(sec int64) {
		t.Helper()
		ts := time.Unix(sec, 0)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	stamp(1_000_000)

	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Files[filepath.Clean(p)].Exists {
		t.Fatal("probe file must be captured")
	}

	// 等长改写：size 不变，只有内容/mtime 变。
	if err := os.WriteFile(p, []byte("BBBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp(2_000_000)

	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Fatalf("等长改写必须靠 mtime 被判为变化; got %#v", changes)
	}
}

// TestDefaultOptions_TracksRealXDevHome 证明默认快照真的盯着开发者真实 xdev home
// 的**用户自有状态**，而不是空列表（票 40 复盘：测试把真实 E:\XDev\config.yaml
// 覆盖掉时，环境变量维度的快照完全看不见）。派生缓存另走 VolatilePaths（只记
// 存在性）——它们本来就会被每次 xdev/shim 调用合法重写（票 43）。无缓存 home 的
// 主机（干净 CI）跳过。
func TestDefaultOptions_TracksRealXDevHome(t *testing.T) {
	home := config.PeekCachedHomeDir()
	if home == "" {
		t.Skip("no cached xdev home on this host")
	}
	opts := DefaultOptions()

	userOwned := make(map[string]bool)
	for _, p := range opts.Paths {
		userOwned[filepath.Clean(p)] = true
	}
	for _, rel := range []string{"config.yaml", "plugins"} {
		want := filepath.Join(home, rel)
		if !userOwned[want] {
			t.Errorf("默认快照未跟踪真实 xdev home 用户自有状态 %s；测试改写它不会被拦下", want)
		}
	}

	volatile := make(map[string]bool)
	for _, p := range opts.VolatilePaths {
		volatile[filepath.Clean(p)] = true
	}
	for _, rel := range []string{".home.cache", filepath.Join("cache", "shim-index.v1")} {
		want := filepath.Join(home, rel)
		if !volatile[want] {
			t.Errorf("派生缓存 %s 未纳入 VolatilePaths；测试创建/删除它不会被发现", want)
		}
		if userOwned[want] {
			t.Errorf("%s 是派生缓存（每次 xdev 调用都会合法重写），不应做 mtime 指纹（会假红）", want)
		}
	}
}

// TestCapture_VolatilePathsIgnoreChurn：派生缓存被合法重写（mtime/size 变）
// 不应算作变化；但它被创建或删除必须被发现。
func TestCapture_VolatilePathsIgnoreChurn(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "home.cache")
	opts := Options{VolatilePaths: []string{p}}

	// 存在 → 重写（等长/变长皆可）→ 不算变化。
	if err := os.WriteFile(p, []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("home-rewritten-by-a-shim-invocation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	if changes := Diff(before, after); len(changes) != 0 {
		t.Errorf("派生缓存重写被判为变化（会假红）: %#v", changes)
	}

	// 删除 → 必须发现。
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	gone, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(after, gone)
	if len(changes) != 1 || changes[0].Kind != "file" {
		t.Errorf("派生缓存被删除必须被发现; got %#v", changes)
	}
	if !strings.Contains(changes[0].After, "missing") {
		t.Errorf("删除后的描述应为 missing; got %q", changes[0].After)
	}
}

// TestCapture_DetectsUserEnvWrite proves SC-001 on platforms where
// SetUserEnvVar persists user environment (Windows registry).
func TestCapture_DetectsUserEnvWrite(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("persistent user env write probe is Windows-only")
	}
	const key = "GOROOT"
	orig, _ := env.GetUserEnvVar(key)
	opts := Options{EnvKeys: []string{key}}

	before, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}

	probe := orig + "__xdev_host_snapshot_probe__"
	if err := env.SetUserEnvVar(key, probe); err != nil {
		t.Fatalf("SetUserEnvVar: %v", err)
	}
	t.Cleanup(func() {
		if orig == "" {
			_ = env.DeleteUserEnvVar(key)
			return
		}
		_ = env.SetUserEnvVar(key, orig)
	})

	after, err := Capture(opts)
	if err != nil {
		t.Fatal(err)
	}
	changes := Diff(before, after)
	if len(changes) == 0 {
		t.Fatal("expected env change after SetUserEnvVar")
	}
	found := false
	for _, c := range changes {
		if c.Kind == "env" && c.Key == key && c.After == probe {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing GOROOT probe change: %#v", changes)
	}
}
