package cli

import (
	"testing"

	"github.com/xmdemt/xdev/internal/config"
)

func newTestCore() config.Config {
	return config.Config{Core: config.CoreConfig{
		SelfUpdate:       true,
		SelfUpdateMode:   "prompt",
		SelfUpdateStale:  true,
	}}
}

// TC-S1-2b coreField/setCoreField roundtrip（cli 层）
func TestSetCoreSelfUpdateFields(t *testing.T) {
	cfg := newTestCore()
	if err := setCoreField(&cfg.Core, "self_update_mode", "auto"); err != nil {
		t.Fatalf("set self_update_mode: %v", err)
	}
	if err := setCoreField(&cfg.Core, "self_update_stale", "false"); err != nil {
		t.Fatalf("set self_update_stale: %v", err)
	}
	if got, err := coreField(&cfg.Core, "self_update_mode"); err != nil || got != "auto" {
		t.Errorf("coreField(self_update_mode) = %q, err=%v, want auto", got, err)
	}
	if got, err := coreField(&cfg.Core, "self_update_stale"); err != nil || got != "false" {
		t.Errorf("coreField(self_update_stale) = %q, err=%v, want false", got, err)
	}
}

// TC-S1-3 非法枚举拒绝
func TestSetCoreSelfUpdateModeInvalid(t *testing.T) {
	cfg := newTestCore()
	if err := setCoreField(&cfg.Core, "self_update_mode", "bogus"); err == nil {
		t.Error("set self_update_mode bogus = nil, want error")
	}
	if cfg.Core.SelfUpdateMode != "prompt" {
		t.Errorf("SelfUpdateMode mutated on invalid set = %q, want prompt unchanged", cfg.Core.SelfUpdateMode)
	}
}

// TC-S1-3b 非法布尔拒绝
func TestSetCoreSelfUpdateStaleInvalid(t *testing.T) {
	cfg := newTestCore()
	if err := setCoreField(&cfg.Core, "self_update_stale", "notabool"); err == nil {
		t.Error("set self_update_stale notabool = nil, want error")
	}
}