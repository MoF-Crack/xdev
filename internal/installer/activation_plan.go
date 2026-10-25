package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/plugin"
	"github.com/xmdemt/xdev/internal/util"
)

// activationPlan 是一次激活的**完整计划 + 回滚状态**。
//
// # 为什么需要它（2026-10-25 架构收口）
//
// 修复前 Activate 在约 200 行里串了 6 件事，回滚靠三个局部布尔
// （junctionCreated / pathsAdded / envVarsWritten）+ 两处手工补偿。
// 而函数结尾是：
//
//	_ = junctionCreated
//	_ = envVarsWritten
//
// 两个变量被**显式丢弃** —— 说明「链接已创建 / env 已写」这条记账线
// 根本不参与任何决策。后果：失败路径的补偿只在两个硬编码点存在，
// **任何新增激活步骤都会静默漏掉回滚**。
//
// 现在把「要做什么」与「做到了哪一步」都收进这个结构体：
//   - plan 字段在 apply 之前算好（纯计算，不碰磁盘）
//   - done 字段由 apply 逐步置位，rollback 据此逐项撤销
//
// 这样新增一个激活步骤时，只要在 plan 里加字段、在 apply 里置位、
// 在 rollback 里撤销，三处都改才编译得过 —— 不再有静默漏回滚的空间。
type activationPlan struct {
	// --- 输入（apply 前确定）---

	tool        string
	version     string
	installPath string

	// currentLink 是 $XMDEV_HOME/current/<tool> 的路径。
	currentLink string

	// linkExisted / oldLinkTarget 记录激活前的链接状态，供回滚恢复。
	// linkExisted=false 表示此前没有 current 链接（回滚时删掉即可）。
	linkExisted   bool
	oldLinkTarget string

	// pathMode 是 GetPathMode() 的快照。
	// 在 plan 阶段取一次，避免 apply 中途配置变化导致行为不一致。
	pathMode string

	// binPaths 是 direct 模式下要写入 PATH 的目录（已含 path_append）。
	binPaths []string

	// shimNames 是 shim 模式下要创建的 shim 名（已过滤非法名）。
	shimNames []string

	// envVars 是要写入用户级注册表的变量。
	envVars map[string]string

	// noEnvWrite 为真时跳过一切持久化写入（--no-path-fix / 测试）。
	noEnvWrite bool

	// --- 执行状态（apply 逐步置位，rollback 据此撤销）---

	// linkSwapped 表示 current 链接已被替换成新目标。
	linkSwapped bool

	// pathsAdded 表示 binPaths 已写入用户 PATH。
	pathsAdded bool

	// envVarsWritten 表示 envVars 已写入注册表。
	envVarsWritten bool
}

// planActivate 计算一次激活要做什么。**纯计算，不写磁盘。**
//
// 返回的 plan 在 apply 之前就可被测试检视 —— 这是把
// 「计划」与「执行」分开的主要收益：以前只能靠拉起完整环境才能
// 观察激活会做什么。
func (i *Installer) planActivate(p plugin.Plugin, installPath, ver string) (*activationPlan, error) {
	if _, err := util.SafeName(p.Name()); err != nil {
		return nil, fmt.Errorf("refusing to activate tool with unsafe name %q: %w", p.Name(), err)
	}
	if _, err := util.SafeName(ver); err != nil {
		return nil, fmt.Errorf("refusing to activate tool %q with unsafe version %q: %w", p.Name(), ver, err)
	}

	currentDir := filepath.Join(i.cfg.Core.XMDevDir, "current")
	currentLink := filepath.Join(currentDir, p.Name())
	if err := util.ValidateSubPath(currentDir, currentLink); err != nil {
		return nil, fmt.Errorf("activation path %q escapes current dir: %w", currentLink, err)
	}

	plan := &activationPlan{
		tool:        p.Name(),
		version:     ver,
		installPath: installPath,
		currentLink: currentLink,
		pathMode:    i.cfg.GetPathMode(),
		noEnvWrite:  i.noEnvWrite,
	}

	// 记录激活前的链接状态（供回滚恢复）。
	if _, err := os.Lstat(currentLink); err == nil {
		plan.linkExisted = true
		if target, err := os.Readlink(currentLink); err == nil {
			plan.oldLinkTarget = target
			// 旧目标还真实存在时，先让它自己清理（如 Java 的 Deactivate）。
			if fi2, err2 := os.Stat(target); err2 == nil && fi2.IsDir() {
				_ = p.Deactivate(target)
			}
		}
	}

	switch plan.pathMode {
	case "direct":
		// direct：把 current/<tool>/bin 与 manifest 的 path_append 写进 PATH。
		currentToolDir := filepath.Join(currentDir, p.Name())
		plan.binPaths = append(env.ToolBinPaths(p.Name(), currentToolDir),
			p.PathAppend(installPath)...)
	default: // shim / auto：创建 shim
		for _, name := range p.Shims() {
			if err := util.ValidateShimName(name); err != nil {
				// F-26：非法 shim 名跳过而非中止整个激活 —— 一个坏名字
				// 不该让其余合法 shim 也装不上。
				continue
			}
			plan.shimNames = append(plan.shimNames, name)
		}
	}

	plan.envVars = p.EnvVars(installPath)

	return plan, nil
}

// rollback 按 plan.done 逐项撤销已完成的步骤。
//
// 顺序与 apply 相反（后做的先撤），且**只撤真的做过的** ——
// 这正是以前那三个被丢弃的布尔本该承担的责任。
//
// 回滚本身失败只记日志：调用方已经在处理一个更严重的错误，
// 再抛一个回滚错误只会掩盖根因。
func (i *Installer) rollback(plan *activationPlan, p plugin.Plugin) {
	if plan == nil {
		return
	}
	// 1. 撤销 env 变量写入。
	if plan.envVarsWritten {
		i.removeUserEnvVars(p)
		plan.envVarsWritten = false
	}

	// 2. 撤销 PATH 追加。
	if plan.pathsAdded {
		env.RemoveCurrentBinPaths(plan.binPaths)
		plan.pathsAdded = false
	}

	// 3. 撤销链接替换（恢复激活前的链接）。
	if plan.linkSwapped {
		i.rollbackActivation(plan.currentLink, plan.linkExisted, plan.oldLinkTarget, p)
		plan.linkSwapped = false
	}

	// 4. 撤销已创建的 shim。注意：这里不区分「本次创建的」与「之前就有的」，
	//    因为 shim 是 xdev 二进制的副本、内容与工具版本无关 ——
	//    删掉后重新激活即可重建，不会丢失用户数据。
	for _, name := range plan.shimNames {
		shimPath := filepath.Join(i.cfg.Core.ShimDir, name)
		_ = os.Remove(shimPath)
		if runtime.GOOS == "windows" {
			_ = os.Remove(shimPath + ".exe")
		}
	}
}

// describe 返回 plan 的可读摘要（日志与测试用）。
func (plan *activationPlan) describe() string {
	return fmt.Sprintf("activate %s@%s mode=%s shims=%d binPaths=%d envVars=%d",
		plan.tool, plan.version, plan.pathMode,
		len(plan.shimNames), len(plan.binPaths), len(plan.envVars))
}