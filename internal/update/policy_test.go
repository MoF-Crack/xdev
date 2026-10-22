package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xmdemt/xdev/internal/config"
)

var testNow = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

func testCore(mode string, selfUpdate bool) *config.CoreConfig {
	return &config.CoreConfig{
		SelfUpdate:      selfUpdate,
		SelfUpdateMode:  mode,
		SelfUpdateStale: true,
	}
}

func fakeCheckResult(hasUpdate bool) *CheckResult {
	return &CheckResult{
		CurrentVersion: "0.3.3",
		LatestVersion:  "9.9.9",
		HasUpdate:      hasUpdate,
		Release:        &ReleaseInfo{TagName: "v9.9.9"},
	}
}

// TC-S2-1 开关关闭：ActionSkip 且 Check 不被调用
func TestCheckPolicy_DisabledNoCall(t *testing.T) {
	called := 0
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			called++
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", false), t.TempDir())
	if act != ActionSkip {
		t.Errorf("action = %d, want ActionSkip", act)
	}
	if called != 0 {
		t.Errorf("Check called %d times, want 0 (disabled)", called)
	}
}

// TC-S2-2 未到检查周期（<24h）：ActionSkip
func TestCheckPolicy_WithinInterval(t *testing.T) {
	cache := t.TempDir()
	if err := writeCheckTime(cache, testNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	called := 0
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			called++
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", true), cache)
	if act != ActionSkip {
		t.Errorf("action = %d, want ActionSkip (within interval)", act)
	}
	if called != 0 {
		t.Errorf("Check called %d times, want 0 (rate limited)", called)
	}
}

// TC-S2-3 到期检查但无更新：ActionSkip
func TestCheckPolicy_ExpiredNoUpdate(t *testing.T) {
	cache := t.TempDir()
	if err := writeCheckTime(cache, testNow.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(false), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, res := p.CheckPolicy(context.Background(), testCore("prompt", true), cache)
	if act != ActionSkip || res.HasUpdate {
		t.Errorf("action=%d hasUpdate=%v, want Skip + no update", act, res.HasUpdate)
	}
}

// TC-S2-4 prompt × 有更新：ActionAsk
func TestCheckPolicy_PromptAsk(t *testing.T) {
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", true), t.TempDir())
	if act != ActionAsk {
		t.Errorf("action = %d, want ActionAsk", act)
	}
}

// TC-S2-5 auto × 有更新：ActionUpdate
func TestCheckPolicy_AutoUpdate(t *testing.T) {
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("auto", true), t.TempDir())
	if act != ActionUpdate {
		t.Errorf("action = %d, want ActionUpdate", act)
	}
}

// TC-S2-6 notify × 有更新：ActionNotify
func TestCheckPolicy_Notify(t *testing.T) {
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("notify", true), t.TempDir())
	if act != ActionNotify {
		t.Errorf("action = %d, want ActionNotify", act)
	}
}

// TC-S2-7 检查失败：ActionSkip + 记录 last_check（下次仍限频）
func TestCheckPolicy_CheckFailureSilent(t *testing.T) {
	cache := t.TempDir()
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return nil, os.ErrDeadlineExceeded
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", true), cache)
	if act != ActionSkip {
		t.Errorf("action = %d, want ActionSkip on failure", act)
	}
	if last, ok := readLastCheck(cache); !ok || !last.Equal(testNow) {
		t.Errorf("lastCheck = %v ok=%v, want %v recorded", last, ok, testNow)
	}
}

// TC-S2-8 已忽略版本 == Latest：ActionSkip（同版本不打扰）
func TestCheckPolicy_IgnoredVersionSkips(t *testing.T) {
	cache := t.TempDir()
	if err := IgnoreVersion(cache, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", true), cache)
	if act != ActionSkip {
		t.Errorf("action = %d, want ActionSkip (ignored)", act)
	}
}

// TC-S2-9 已忽略旧版本 < Latest：ActionAsk（新版本重新提示）
func TestCheckPolicy_NewerVersionReasks(t *testing.T) {
	cache := t.TempDir()
	if err := IgnoreVersion(cache, "9.8.0"); err != nil {
		t.Fatal(err)
	}
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("prompt", true), cache)
	if act != ActionAsk {
		t.Errorf("action = %d, want ActionAsk (newer version)", act)
	}
}

// TC-S2-10 未知 mode 容错为 prompt
func TestCheckPolicy_UnknownModeFallsBackToPrompt(t *testing.T) {
	p := &Policy{
		Check: func(ctx context.Context, cur string) (*CheckResult, error) {
			return fakeCheckResult(true), nil
		},
		Now: func() time.Time { return testNow },
	}
	act, _ := p.CheckPolicy(context.Background(), testCore("bogus", true), t.TempDir())
	if act != ActionAsk {
		t.Errorf("action = %d, want ActionAsk (fallback prompt)", act)
	}
}

// 忽略记录文件独立可读
func TestIgnoreVersionPersists(t *testing.T) {
	cache := t.TempDir()
	if err := IgnoreVersion(cache, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, ignoreFile)); err != nil {
		t.Fatalf("ignore file not written: %v", err)
	}
	if v, ok := readIgnoredVersion(cache); !ok || v != "0.4.0" {
		t.Errorf("readIgnoredVersion = %q ok=%v, want 0.4.0", v, ok)
	}
}
