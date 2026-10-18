//go:build windows

// transform_path_length_a12_test.go — 第九轮审计 A-12（P2）复现守卫。
//
// 缺陷原形：WriteUserPath（→ writeUserPathFromRegistry）有 M-2 长度闸门
// （>32767 拒绝），但 **TransformUserPath / TransformSystemPath 直接
// `WriteUserPath(next)`**：真实后端下这条闸门会拦（只是错误文案对不上），
// 而在**内存/替身后端**（测试、未来的可插拔后端）下长度检查完全不存在 ——
// 实测可写入 40000 字符 PATH。校验必须在 transform 入口这一层，与
// writeUserPathFromRegistry 的判据/文案口径一致。
package env

import (
	"strconv"
	"strings"
	"testing"
)

const a12MaxPathLen = 32767

// TestTransformUserPath_RejectsOverlongPath：超限时不得落盘，且错误文案
// 与 writeUserPathFromRegistry 的 M-2 口径一致（含长度与上限两个数字）。
func TestTransformUserPath_RejectsOverlongPath(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	const initial = `C:\Windows\System32`
	if err := WriteUserPath(initial); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	overlong := `C:\Windows\System32;` + strings.Repeat("X", 40000)
	changed, err := TransformUserPath(func(cur string) (string, bool, error) {
		return overlong, true, nil
	})
	if err == nil {
		t.Fatal("TransformUserPath 必须拒绝超过 32767 字符的 PATH（A-12：缺 M-2 长度校验）")
	}
	if changed {
		t.Error("被拒绝的写入不得报告 changed=true")
	}
	if !strings.Contains(err.Error(), "32767") {
		t.Errorf("错误文案必须点明 32767 上限（与 writeUserPathFromRegistry 口径一致）: %v", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(len(overlong))) {
		t.Errorf("错误文案必须点明实际长度 %d（与 writeUserPathFromRegistry 口径一致）: %v", len(overlong), err)
	}

	if got, _ := ReadUserPath(); got != initial {
		t.Errorf("被拒绝时后端值必须不变:\n want=%q\n got =%q", initial, got)
	}
}

// TestTransformUserPath_AcceptsBoundaryLength：恰好 32767 是合法的
// （writeUserPathFromRegistry 用 `>` 而非 `>=`），不得误伤。
func TestTransformUserPath_AcceptsBoundaryLength(t *testing.T) {
	original := saveUserPath(t)
	defer restoreUserPath(t, original)

	if err := WriteUserPath(`C:\Windows\System32`); err != nil {
		t.Fatalf("WriteUserPath: %v", err)
	}

	exact := strings.Repeat("Y", a12MaxPathLen)
	changed, err := TransformUserPath(func(cur string) (string, bool, error) {
		return exact, true, nil
	})
	if err != nil {
		t.Fatalf("恰好 %d 字符必须被接受（与 writeUserPathFromRegistry 的 `>` 判据一致）: %v", a12MaxPathLen, err)
	}
	if !changed {
		t.Error("边界长度的真实变更应报告 changed=true")
	}
	if got, _ := ReadUserPath(); got != exact {
		t.Errorf("边界长度写入未生效: len(got)=%d", len(got))
	}
}

// TestWriteUserPathFromRegistry_LengthMessageParity：把真身文案固定下来，
// 使 transform 侧与之对齐时有单一来源可查（真身调用零落盘——长度校验先于
// 任何注册表访问，闸门计数已在 userenv_hermetic_test.go 声明）。
func TestWriteUserPathFromRegistry_LengthMessageParity(t *testing.T) {
	expectedRealRegistryMutationCount.Add(1) // 零落盘：长度拒绝先于注册表访问
	err := writeUserPathFromRegistry(strings.Repeat("X", a12MaxPathLen+1))
	if err == nil {
		t.Fatal("真身必须拒绝超限 PATH")
	}
	for _, want := range []string{"32767", "32768"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("真身文案缺少 %q: %v", want, err)
		}
	}
}
