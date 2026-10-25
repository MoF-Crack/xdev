package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// 回归守卫（2026-10-25 架构收口）：Activate 的回滚记账不得再被丢弃。
//
// 修复前 installer.go 的 Activate 结尾是：
//
//	_ = junctionCreated
//	_ = envVarsWritten
//
// 两个变量被显式丢弃，说明「链接已创建 / env 已写」这条记账线不参与
// 任何决策 —— 失败路径的补偿只在两个硬编码点存在，任何新增激活步骤
// 都会静默漏掉回滚。
//
// 现在状态全在 activationPlan 里，rollback 据此逐项撤销。本守卫用源码
// 级断言锁住这个结构，防止有人图省事又写回 `_ = xxx`。
func TestActivate_NoDiscardedRollbackState(t *testing.T) {
	data, err := os.ReadFile("installer.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	// 找出 Activate 函数体（到下一个顶层 func 为止）。
	idx := strings.Index(src, "func (i *Installer) Activate(")
	if idx < 0 {
		t.Fatal("Activate 函数未找到")
	}
	rest := src[idx:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := stripComments(rest[:end])

	for _, banned := range []string{"_ = junctionCreated", "_ = envVarsWritten", "_ = pathsAdded"} {
		if strings.Contains(body, banned) {
			t.Errorf("Activate 不得丢弃回滚状态 %q —— 状态必须由 activationPlan 承载，"+
				"rollback 据此逐项撤销", banned)
		}
	}

	// 必须真的用 plan/rollback。
	if !strings.Contains(body, "planActivate(") {
		t.Error("Activate 必须通过 planActivate 计算计划")
	}
	if !strings.Contains(body, "i.rollback(plan") {
		t.Error("Activate 的失败路径必须调 i.rollback(plan, p) 统一回滚")
	}
}

// stripComments 去掉整行注释与行尾注释，避免守卫匹配到解释性文字。
func stripComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			b.WriteString("\n")
			continue
		}
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// TestActivationPlan_RecordsSteps 确认 plan 会把各步骤记录下来，
// 且 rollback 只撤销「真的做过的」。
func TestActivationPlan_RecordsSteps(t *testing.T) {
	ins, cfg, reg := newTestInstaller(t)
	_ = reg

	tool := &manifest.Tool{
		Name:        "plantest",
		DisplayName: "Plan Test",
		Shims:       []string{"plantest"},
		EnvVars:     map[string]string{"PLAN_TEST_HOME": "{{install}}"},
	}
	p := plugin.NewGeneric(tool)

	installPath := filepath.Join(cfg.Core.InstallDir, "plantest", "1.0.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatal(err)
	}

	plan, err := ins.planActivate(p, installPath, "1.0.0")
	if err != nil {
		t.Fatalf("planActivate: %v", err)
	}

	// 计划阶段不应有任何执行状态被置位。
	if plan.linkSwapped || plan.pathsAdded || plan.envVarsWritten {
		t.Errorf("plan 阶段不应置位执行状态: %+v", plan)
	}
	// 但应记录下要做什么。
	if len(plan.envVars) == 0 {
		t.Error("plan 应记录 envVars")
	}
	if plan.currentLink == "" {
		t.Error("plan 应记录 currentLink")
	}
	if plan.pathMode == "" {
		t.Error("plan 应记录 pathMode")
	}
}

// TestActivationPlan_RejectsUnsafeName 确认安全校验在 plan 阶段就 fail-closed。
func TestActivationPlan_RejectsUnsafeName(t *testing.T) {
	ins, _, _ := newTestInstaller(t)

	tool := &manifest.Tool{Name: `..\evil`, DisplayName: "Evil"}
	p := plugin.NewGeneric(tool)

	if _, err := ins.planActivate(p, "/tmp/x", "1.0.0"); err == nil {
		t.Error("非法工具名必须在 plan 阶段被拒绝")
	}
}

// TestActivationPlan_RejectsUnsafeVersion 同上，针对版本号。
func TestActivationPlan_RejectsUnsafeVersion(t *testing.T) {
	ins, _, _ := newTestInstaller(t)

	tool := &manifest.Tool{Name: "safe", DisplayName: "Safe"}
	p := plugin.NewGeneric(tool)

	if _, err := ins.planActivate(p, "/tmp/x", "../../evil"); err == nil {
		t.Error("非法版本号必须在 plan 阶段被拒绝")
	}
}
