//go:build windows

// Ticket 05 (audit-fixes / P0-07 + N-10) guard tests: RegEnumValueW
// robustness in GetAllUserEnvVars and EnumEnvVars.
//
// The historical bugs: GetAllUserEnvVars used a 256-char name buffer and,
// on ERROR_MORE_DATA, sliced nameBuf[:nameBufSize] with the API-written
// required length > 256 → panic (slice bounds out of range) on every
// Windows shim execution. EnumEnvVars passed the wrong byte size
// (len(buf) instead of len(buf)*2) and silently broke the whole
// enumeration on values larger than the buffer.
//
// 第四轮复核后的双模式说明：userenv_hermetic_test.go 的 TestMain 默认为整包安装
// 内存用户环境后端，本文件的探针写入落在内存里 —— R1/R2 默认验证的是接缝层与
// 内存后端的长名字处理；真实 RegEnumValueW 缓冲区增长路径（P0-07 的 panic 修复、
// N-10 的缓冲重建）只有在 XMDEV_ALLOW_REAL_REGISTRY_TESTS=1（真实后端，探针写入
// 真实注册表并随 t.Cleanup 清理）时才被完整驱动。CI runner 是一次性环境，若未来
// 加 CI 测试 job 可设置该变量换取全量真身覆盖；开发机默认保持宿主零接触。
package env

import (
	"os"
	"strings"
	"testing"
)

// setLongTestVar writes a registry value through the trusted path (the
// 300-char names used here are not in the write whitelist) and registers
// cleanup.
func setLongTestVar(t *testing.T, name, value string) {
	t.Helper()
	if err := SetUserEnvVarTrusted(name, value); err != nil {
		t.Fatalf("SetUserEnvVarTrusted(%d-char name): %v", len(name), err)
	}
	t.Cleanup(func() {
		if err := DeleteUserEnvVarTrusted(name); err != nil {
			t.Errorf("cleanup DeleteUserEnvVarTrusted(%d-char name): %v", len(name), err)
		}
	})
}

// R1: a 300-character value name must not panic GetAllUserEnvVars (the
// hot path of every Windows shim execution) and must be returned whole.
func TestGetAllUserEnvVars_LongValueName_NoPanic(t *testing.T) {
	const totalLen = 300
	name := "XDEVTESTREGENUM" + strings.Repeat("A", totalLen-15) // 15-char prefix + padding
	if len(name) != totalLen {
		t.Fatalf("test name length = %d, want %d", len(name), totalLen)
	}
	setLongTestVar(t, name, "longname-value")

	all := GetAllUserEnvVars() // previously: panic: slice bounds out of range
	got, ok := all[name]
	if !ok || got != "longname-value" {
		t.Errorf("300-char value name not enumerated correctly: present=%v value=%q", ok, got)
	}
}

// R2: a name of exactly 256 chars (the old buffer size — the historical
// boundary case) must not panic either. With the retry it is returned whole.
func TestGetAllUserEnvVars_Exactly256_TruncatedNotPanic(t *testing.T) {
	name := "XDEVTESTREGENUM256" + strings.Repeat("B", 256-18)
	if len(name) != 256 {
		t.Fatalf("test name length = %d, want 256", len(name))
	}
	setLongTestVar(t, name, "boundary-value")

	all := GetAllUserEnvVars() // must not panic
	got, ok := all[name]
	if !ok || got != "boundary-value" {
		t.Errorf("256-char value name not enumerated correctly: present=%v value=%q", ok, got)
	}
}

// R3: value data larger than the 64 KiB buffer must not silently truncate
// the EnumEnvVars enumeration — the value is returned whole and the rest
// of the enumeration survives (a small marker proves the list is not
// cut short).
//
// 只能在真实注册表后端下运行：EnumEnvVars 直连 RegEnumValueW（不走
// getAllUserEnvVarsFn 接缝），N-10 的缓冲重建逻辑必须由**真实的大值**驱动；
// 而默认防污染模式下探针写入落在内存后端里，真实注册表读不到 —— 硬跑必然
// 假红。因此默认模式显式跳过（不是静默通过），以 XMDEV_ALLOW_REAL_REGISTRY_TESTS=1
// 运行时写入真实注册表（带 t.Cleanup 清理）并全量验证。
func TestEnumEnvVars_LargeValueData(t *testing.T) {
	if os.Getenv("XMDEV_ALLOW_REAL_REGISTRY_TESTS") == "" {
		t.Skip("EnumEnvVars 真身路径（RegEnumValueW 缓冲重建，N-10）需要真实注册表数据；默认宿主零接触模式下跳过。以 XMDEV_ALLOW_REAL_REGISTRY_TESTS=1 运行以执行完整验证")
	}

	bigValue := strings.Repeat("C", 70000) // 70,000 chars ≈ 140 KB UTF-16
	setLongTestVar(t, "XDEVTESTREGENUMBIG", bigValue)
	setLongTestVar(t, "XDEVTESTREGENUMSMALL", "small-marker")

	pairs, err := EnumEnvVars("user")
	if err != nil {
		t.Fatalf("EnumEnvVars: %v", err)
	}

	var big, small bool
	for _, p := range pairs {
		switch p.Name {
		case "XDEVTESTREGENUMBIG":
			big = true
			if len(p.Value) != len(bigValue) || !strings.HasPrefix(p.Value, "CCC") {
				t.Errorf("large value truncated: got %d chars, want %d", len(p.Value), len(bigValue))
			}
		case "XDEVTESTREGENUMSMALL":
			small = true
			if p.Value != "small-marker" {
				t.Errorf("small marker value = %q, want %q", p.Value, "small-marker")
			}
		}
	}
	if !big {
		t.Error("70KB value missing from EnumEnvVars result (silent truncation regression)")
	}
	if !small {
		t.Error("enumeration aborted after the large value — the list must not be cut short")
	}
}
