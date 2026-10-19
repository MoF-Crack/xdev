// quarantine_unique_r10_test.go 复现第十轮 P3-1（P3）：
// **quarantine 文件名只有秒级精度，同秒两次会互相覆盖**。
//
// 缺陷现场（persist.go:177-183）：
//
//	dst := fmt.Sprintf("%s.corrupt-%s", p, time.Now().UTC().Format("20060102T150405Z"))
//
// 秒级时间戳在同一秒内的两次 quarantine 上完全相同 → os.Rename 覆盖前一份证据。
// 审计实测：同秒两次 → `quarantine_files=1`，内容只剩第二次的，**第一份里泄漏
// 容器的 ID 丢失**。损坏的 state 文件是残留资源的唯一线索，覆盖等于销毁证据。
//
// 修复要求：纳秒/随机后缀，或用 os.OpenFile(O_CREATE|O_EXCL) 保证不覆盖。
package isolator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestR10_QuarantineKeepsBothEvidenceFiles：同一秒内两次 quarantine 必须产生
// 两个文件，且第一份证据的内容仍在（不得被第二次覆盖）。
func TestR10_QuarantineKeepsBothEvidenceFiles(t *testing.T) {
	home := t.TempDir()
	p := statePath(home, string(ModeContainer))

	// 第一次：泄漏容器 A 的句柄。
	first := `{"containerId": "leaked-container-A", "image": "ubuntu:22.04"` // 截断 JSON
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	quarantineState(home, string(ModeContainer))

	// 第二次：泄漏容器 B 的句柄（同一秒内）。
	second := `{"containerId": "leaked-container-B", "image": "ubuntu:22.04"`
	if err := os.WriteFile(p, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	quarantineState(home, string(ModeContainer))

	matches, err := filepath.Glob(p + ".corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("P3-1 复现：同秒两次 quarantine 必须留下 2 份证据文件，got %d 个: %v "+
			"（第一份里的泄漏容器 ID 已被覆盖销毁）", len(matches), matches)
	}
	seen := map[string]bool{}
	for _, m := range matches {
		seen[readFileT(t, m)] = true
	}
	if !seen[first] {
		t.Errorf("P3-1 复现：第一份 quarantine 证据（%s）被第二次覆盖，丢失了泄漏容器的线索", first)
	}
	if !seen[second] {
		t.Errorf("第二份 quarantine 证据缺失: %s", second)
	}
}

// TestR10_QuarantineHintNamesAnExistingFile：返回给用户的说明必须指向真实存在
// 的文件（修复引入唯一后缀后，提示里的路径不能是猜的）。
func TestR10_QuarantineHintNamesAnExistingFile(t *testing.T) {
	home := t.TempDir()
	p := writeCorruptState(t, home, string(ModeContainer), corruptContainerState)

	hint := quarantineState(home, string(ModeContainer))
	if !strings.Contains(hint, p+".corrupt-") {
		t.Fatalf("提示必须给出 quarantine 文件路径，got %q", hint)
	}
	// 从提示里取出路径并确认它存在。
	idx := strings.Index(hint, p+".corrupt-")
	name := hint[idx:]
	if end := strings.IndexAny(name, " ;,"); end >= 0 {
		name = name[:end]
	}
	if !fileExists(name) {
		t.Errorf("提示里的文件 %s 不存在，hint = %q", name, hint)
	}
}

// TestR10_QuarantineTwiceIsIdempotentWhenNoFile：没有损坏文件时 quarantine 只是
// 给出说明，不报错、不创建文件（重复调用安全）。
func TestR10_QuarantineTwiceIsIdempotentWhenNoFile(t *testing.T) {
	home := t.TempDir()
	first := quarantineState(home, string(ModeContainer))
	second := quarantineState(home, string(ModeContainer))
	if first != second {
		t.Errorf("无文件时两次 quarantine 说明应一致，got %q / %q", first, second)
	}
	if matches, _ := filepath.Glob(statePath(home, string(ModeContainer)) + ".corrupt-*"); len(matches) != 0 {
		t.Errorf("无损坏文件时不得凭空创建 quarantine 文件: %v", matches)
	}
}
