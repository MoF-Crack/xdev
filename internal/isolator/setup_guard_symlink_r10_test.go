// setup_guard_symlink_r10_test.go 复现第十轮 P3-3（P3）：
// **`--force` 会跟随符号链接覆盖项目外文件**。
//
// 缺陷现场（setup_guard.go:18-33）：ensureOverwritable 用 os.Stat（**跟随**链接）
// 判断「已存在」。链接目标存在时它把 symlink 当成普通已存在文件，`--force`
// 一放行，nix.go:245 / container.go:243 的 os.WriteFile（O_TRUNC）就顺着链接写到
// **项目外**的目标文件上；链接目标不存在（悬空链接）时 os.Stat 返回 ENOENT，
// 门禁甚至直接放行。
//
// 审计实测：flake.nix 是指向项目外 victim 的符号链接，Force:true Setup 之后
//
//	victim_overwritten=true flake_still_symlink=true
//
// 即：一个本意只是「覆盖项目内生成文件」的开关，变成了「任意文件写」原语。
//
// 修复要求：用 Lstat 检测并**拒绝** symlink（或 O_CREATE|O_EXCL）。
package isolator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const outsideVictimContent = "SECRET — must never be touched by xdev\n"

// symlinkOrSkip 建立符号链接；环境不支持（Windows 无开发者模式/权限、部分文件
// 系统）时跳过用例，而不是误报失败。
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("此环境无法创建符号链接（Windows 需开发者模式或管理员权限）：%v", err)
	}
}

// TestR10_NixSetupForceRefusesSymlinkedFlake：--force 不得顺着 symlink 覆盖项目外文件。
func TestR10_NixSetupForceRefusesSymlinkedFlake(t *testing.T) {
	home, proj, outside := t.TempDir(), t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "victim.nix")
	writeFileT(t, victim, outsideVictimContent)
	flake := filepath.Join(proj, "flake.nix")
	symlinkOrSkip(t, victim, flake)

	n := &nixIsolator{xmdeDir: home}
	err := n.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj, Force: true})
	if err == nil {
		t.Error("P3-3 复现：--force 跟随符号链接放行，项目外的 victim 会被 os.WriteFile 截断覆盖")
	} else if !strings.Contains(strings.ToLower(err.Error()), "symlink") &&
		!strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("拒绝理由必须点明是符号链接，got: %v", err)
	}

	if got := readFileT(t, victim); got != outsideVictimContent {
		t.Errorf("P3-3 复现：项目外文件被 xdev 覆盖，内容 %q（原为 %q）", got, outsideVictimContent)
	}
	// 链接本身必须原样保留（xdev 既不写它也不删它）。
	fi, statErr := os.Lstat(flake)
	if statErr != nil {
		t.Fatalf("flake.nix 链接被删除: %v", statErr)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("flake.nix 必须是原样的符号链接（xdev 不得把它替换成普通文件）")
	}
}

// TestR10_NixSetupRefusesDanglingSymlinkWithoutForce：悬空链接更隐蔽——
// os.Stat 返回 ENOENT，修复前的门禁**直接放行**（连 --force 都不需要），
// os.WriteFile 于是顺着链接在项目外创建/写入目标文件。
func TestR10_NixSetupRefusesDanglingSymlinkWithoutForce(t *testing.T) {
	home, proj, outside := t.TempDir(), t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "created-by-xdev.nix")
	flake := filepath.Join(proj, "flake.nix")
	symlinkOrSkip(t, target, flake)

	n := &nixIsolator{xmdeDir: home}
	err := n.Setup(context.Background(), &EnvironmentSpec{WorkingDir: proj}) // 注意：没有 --force
	if err == nil {
		t.Error("P3-3 复现：悬空符号链接被 os.Stat 判为「不存在」而放行，写操作会落到项目外")
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Errorf("P3-3 复现：xdev 顺着悬空链接在项目外创建了 %s", target)
	}
}

// TestR10_ContainerGenerateDevcontainerRefusesSymlinkedTarget：container 侧同源
// （container.go:243 的 os.WriteFile 有同样问题）。
func TestR10_ContainerGenerateDevcontainerRefusesSymlinkedTarget(t *testing.T) {
	home, proj, outside := t.TempDir(), t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "victim.json")
	writeFileT(t, victim, outsideVictimContent)
	dcDir := filepath.Join(proj, ".devcontainer")
	if err := os.MkdirAll(dcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dc := filepath.Join(dcDir, "devcontainer.json")
	symlinkOrSkip(t, victim, dc)

	c := &containerIsolator{xmdeDir: home, projectDir: proj,
		spec: &EnvironmentSpec{Env: map[string]string{}, Force: true}}
	err := c.generateDevcontainer()
	if err == nil {
		t.Error("P3-3 复现：--force 跟随 devcontainer.json 符号链接覆盖项目外文件")
	}
	if got := readFileT(t, victim); got != outsideVictimContent {
		t.Errorf("P3-3 复现：项目外文件被覆盖，内容 %q", got)
	}
}

// TestR10_EnsureOverwritableRejectsSymlink：门禁本身的最小契约——无论 force
// 与否，符号链接一律拒绝，且错误信息点明原因。
func TestR10_EnsureOverwritableRejectsSymlink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	writeFileT(t, victim, outsideVictimContent)
	link := filepath.Join(dir, "link.txt")
	symlinkOrSkip(t, victim, link)

	for _, force := range []bool{false, true} {
		err := ensureOverwritable(link, force)
		if err == nil {
			t.Errorf("force=%v 时必须拒绝符号链接（os.Stat 跟随链接会把「已存在」判成普通文件）", force)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "symlink") &&
			!strings.Contains(err.Error(), "symbolic link") {
			t.Errorf("force=%v：错误必须点明符号链接，got: %v", force, err)
		}
	}
	if got := readFileT(t, victim); got != outsideVictimContent {
		t.Errorf("门禁不得改动链接目标，内容 %q", got)
	}
}

// TestR10_EnsureOverwritableStillAllowsRegularForceOverwrite：防「修复把正常
// 路径弄红」——普通文件 + --force 仍必须放行，不存在的路径仍必须放行。
func TestR10_EnsureOverwritableStillAllowsRegularForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.txt")
	writeFileT(t, regular, "user content\n")

	if err := ensureOverwritable(regular, true); err != nil {
		t.Errorf("普通文件 + --force 必须放行，got: %v", err)
	}
	if err := ensureOverwritable(regular, false); err == nil {
		t.Error("普通文件 + 无 --force 必须拒绝")
	}
	if err := ensureOverwritable(filepath.Join(dir, "missing.txt"), false); err != nil {
		t.Errorf("不存在的路径必须放行，got: %v", err)
	}
}
