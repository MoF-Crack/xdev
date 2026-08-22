// Spec 034: 启动自检策略（internal/update/policy.go）。
//
// CLI（TUI/GUI 交互入口）与 GUI 共用：读配置 + 忽略/频率记录，
// 输出一个 Action，交互层各自实现三选提示/应用/退出。
// 任何错误静默降级为 ActionSkip——启动流程绝不被更新检查阻塞。
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/version"
)

// Action 描述一次启动自检的决策。
type Action int

const (
	// ActionSkip 开关关闭 / 未到检查周期 / 检查失败 / 已忽略同版本 —— 静默继续。
	ActionSkip Action = iota
	// ActionAsk mode=prompt 且有新版本：交给交互层三选（更新/忽略并继续/退出）。
	ActionAsk
	// ActionUpdate mode=auto：直接应用更新（复用 Apply）。
	ActionUpdate
	// ActionNotify mode=notify 或非 TTY 降级：仅提示存在更新。
	ActionNotify
)

const (
	selfUpdateCheckInterval = 24 * time.Hour
	lastCheckFile           = "self_update_last_check.json"
	ignoreFile              = "self_update_ignore.json"
)

// Policy 携带依赖注入点（Check 网络探测、Now 时钟），测试替换为 fake。
type Policy struct {
	Check func(ctx context.Context, currentVersion string) (*CheckResult, error)
	Now   func() time.Time
}

// NewPolicy 返回生产策略：真实 Check（GitHub Releases）+ 系统时钟。
func NewPolicy() *Policy {
	return &Policy{Check: Check, Now: time.Now}
}

// CheckPolicy 读取配置与缓存（限频/忽略），返回动作与检查结果。
// cacheDir 通常为 cfg.Core.CacheDir。
func (p *Policy) CheckPolicy(ctx context.Context, core *config.CoreConfig, cacheDir string) (Action, *CheckResult) {
	if !core.SelfUpdate {
		return ActionSkip, nil
	}

	// 频率保护：24h 内不重复检查（启动型入口每次都可能触发）。
	if last, ok := readLastCheck(cacheDir); ok && p.Now().Sub(last) < selfUpdateCheckInterval {
		return ActionSkip, nil
	}

	result, err := p.Check(ctx, version.Version)
	// 无论成败都记录检查时间：失败也限频，避免每次启动都打 GitHub API。
	_ = writeCheckTime(cacheDir, p.Now())
	if err != nil {
		log.Printf("[update] startup check failed (silent): %v", err)
		return ActionSkip, nil
	}
	if !result.HasUpdate {
		return ActionSkip, result
	}

	switch core.SelfUpdateMode {
	case "auto":
		return ActionUpdate, result
	case "notify":
		return ActionNotify, result
	default: // "prompt" 及未知值容错为 prompt
		if ignored, ok := readIgnoredVersion(cacheDir); ok && ignored == result.LatestVersion {
			return ActionSkip, result // 同一版本已忽略：不再打扰
		}
		return ActionAsk, result
	}
}

// IgnoreVersion 记录用户「忽略并继续」的版本（prompt 三选中的 Ignore）。
// 下一次 CheckPolicy 对同一最新版本返回 ActionSkip；新版本出现则重新提示。
func IgnoreVersion(cacheDir, version string) error {
	return writeJSONAtomic(filepath.Join(cacheDir, ignoreFile), map[string]string{
		"version":   version,
		"ignoredAt": time.Now().Format(time.RFC3339),
	})
}

// readIgnoredVersion 读取已忽略版本；无记录返回 "", false。
func readIgnoredVersion(cacheDir string) (string, bool) {
	var rec struct {
		Version string `json:"version"`
	}
	if err := readJSON(filepath.Join(cacheDir, ignoreFile), &rec); err != nil {
		return "", false
	}
	if rec.Version == "" {
		return "", false
	}
	return rec.Version, true
}

// readLastCheck 读取上次检查时间；无记录返回 ok=false。
func readLastCheck(cacheDir string) (time.Time, bool) {
	var rec struct {
		CheckedAt time.Time `json:"checkedAt"`
	}
	if err := readJSON(filepath.Join(cacheDir, lastCheckFile), &rec); err != nil {
		return time.Time{}, false
	}
	if rec.CheckedAt.IsZero() {
		return time.Time{}, false
	}
	return rec.CheckedAt, true
}

func writeCheckTime(cacheDir string, at time.Time) error {
	return writeJSONAtomic(filepath.Join(cacheDir, lastCheckFile), map[string]time.Time{
		"checkedAt": at,
	})
}

// readJSON 读取并解码 JSON 文件；文件缺失/损坏均返回错误（读方容错）。
func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSONAtomic 原子写 JSON：临时文件 + rename（与 gui cache 同模式，防并发撕裂）。
func writeJSONAtomic(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".self-update-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := json.NewEncoder(tmp).Encode(v); err != nil {
		tmp.Close()
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	return os.Rename(tmpName, path)
}