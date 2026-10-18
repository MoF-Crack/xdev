//go:build windows

// userpath_lower_bound_p23_test.go — 第九轮审计 P2-3（P2）复现守卫。
//
// 缺陷原形：WriteUserPath 的长度闸门只有**上界**（M-2：>32767 拒绝），没有下界。
// 当用户的 user PATH 恰好只由 xdev 自有条目组成时，清理/移除流程会算出空字符串
// 并原样写盘（实测三个场景产出 Path=""），随后 BroadcastEnvironmentChange 把空值
// 广播出去 —— 之后新建进程再也看不到任何**用户级** PATH 条目。
//
// 诚实标注可达性：HKLM 的系统 PATH 仍然生效，所以后果是「丢失全部用户级条目」
// 而非「完全没有 PATH」；且需要「PATH 恰好只由 xdev 条目组成」这一前提，非无条件
// 可达。但「写入值不得退化为空」这条不变量确实缺失。
//
// 本守卫钉住不变量：所有 PATH 写入的唯一收口（WriteUserPath）拒绝空值。
package env

import (
	"strings"
	"testing"
)

// TestWriteUserPath_RejectsEmptyValue：空值必须在写入前被拒绝。
func TestWriteUserPath_RejectsEmptyValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"empty string", ""},
		{"single space", " "},
		{"whitespace only", "   \t  "},
		{"newline only", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := WriteUserPath(tc.value)
			if err == nil {
				t.Error("P2-3 复现：空/纯空白的 PATH 被写入了（下界不变量缺失）——" +
					"新建进程将丢失全部用户级 PATH 条目")
				return
			}
			if !strings.Contains(err.Error(), "empty user PATH") {
				t.Errorf("错误信息应说明是空 PATH，got: %v", err)
			}
		})
	}
}

// TestWriteUserPath_AcceptsNonEmptyValue：正常值仍照常工作（修复不得把正常路径弄红）。
//
// 显式安装一层本用例自己的内存后端（而不是依赖 TestMain 的种子后端）：
// `UseInMemoryUserPathForTesting` 的还原语义是「还原到安装前的接缝值」，而整包
// 测试里存在用例级安装/还原的嵌套；若上级后端已被还原，闭包会持有已置 nil 的
// store（`inMemoryUserPath = nil`）→ 调用时 nil 解引用。这是**既有测试卫生的脆弱
// 点**（与本次 P2-3 无关，已如实登记），本用例通过自行安装后端来规避。
func TestWriteUserPath_AcceptsNonEmptyValue(t *testing.T) {
	get, restore := UseInMemoryUserPathForTesting("")
	defer restore()

	if err := WriteUserPath(`C:\Tools\bin;%USERPROFILE%\bin`); err != nil {
		t.Fatalf("非空 PATH 应被接受: %v", err)
	}
	if got := get(); !strings.Contains(got, `C:\Tools\bin`) {
		t.Errorf("写入未生效: %q", got)
	}
}

// TestCleanupUserPath_DoesNotEmitEmptyValue：清理 xdev 条目时，若结果会退化为
// 空值，必须报错（可诊断）而不是静默写空。
//
// 场景：user PATH 只含 xdev 的 shim 目录。
func TestCleanupUserPath_DoesNotEmitEmptyValue(t *testing.T) {
	home := `C:\XDevHome`
	shim := `C:\XDevHome\shims`

	// 同 TestWriteUserPath_AcceptsNonEmptyValue：自行安装后端，规避上级后端已被
	// 还原后闭包持有 nil store 的既有脆弱点。
	_, restore := UseInMemoryUserPathForTesting("")
	defer restore()

	// 内存后端写入「只由 xdev 条目组成」的 PATH。
	if err := WriteUserPath(shim); err != nil {
		t.Fatalf("seed PATH: %v", err)
	}

	_, err := CleanupUserPath(home, shim, "shim")
	// 两种可接受结果：
	//   (a) 返回错误（WriteUserPath 拒绝空值）；
	//   (b) 成功但 PATH 仍非空（实现选择保留某个条目）。
	// 不可接受：成功且 PATH 被写成空。
	if err == nil {
		got, rerr := ReadUserPath()
		if rerr != nil {
			t.Fatalf("ReadUserPath: %v", rerr)
		}
		if strings.TrimSpace(got) == "" {
			t.Error("P2-3 复现：CleanupUserPath 把 user PATH 清成了空值且未报错")
		}
	} else if !strings.Contains(err.Error(), "empty user PATH") {
		t.Logf("注意：CleanupUserPath 返回了非「空 PATH」类错误（可接受，但请确认不是别的回归）: %v", err)
	}
}
