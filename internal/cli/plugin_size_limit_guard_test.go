package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 40（P2-17 / E-13）：4 MiB 上限必须「检测截断」而不是静默腰斩
// ---------------------------------------------------------------------------
//
// 旧实现 `io.Copy(f, io.LimitReader(resp.Body, 4<<20))`：超过 4 MiB 时
// LimitReader 报 EOF，io.Copy 返回 nil —— 半截脚本被当成下载成功落盘，
// 随后 Lua 校验失败，把「响应超限」伪装成语法错误。downloadPluginBody
// 现在在拷贝之后探测是否还有剩余字节并明确报错、清理半截文件。

// TestDownloadPluginBody_OversizeIsRejected 超限响应 → 明确报错 + 半截文件被清理。
func TestDownloadPluginBody_OversizeIsRejected(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "evil.lua")
	body := bytes.NewReader(make([]byte, maxPluginSize+1))

	err := downloadPluginBody(dest, body)
	if err == nil {
		t.Fatal("downloadPluginBody(4MiB+1) = nil, want explicit over-limit error")
	}
	if !strings.Contains(err.Error(), "exceeds the 4 MiB limit") {
		t.Errorf("错误文案未指出超限原因: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("超限后仍留下半截插件文件（既有清理语义被破坏）: stat err = %v", statErr)
	}
}

// TestDownloadPluginBody_ExactLimitIsAccepted 恰好 4 MiB 不算超限（边界值）。
func TestDownloadPluginBody_ExactLimitIsAccepted(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "big.lua")
	body := bytes.NewReader(make([]byte, maxPluginSize))

	if err := downloadPluginBody(dest, body); err != nil {
		t.Fatalf("downloadPluginBody(恰好 4 MiB) = %v, want nil", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() != int64(maxPluginSize) {
		t.Errorf("落盘大小 = %d, want %d", fi.Size(), int64(maxPluginSize))
	}
}

// TestDownloadPluginBody_SmallPluginIsWrittenVerbatim 正常插件内容原样落盘。
func TestDownloadPluginBody_SmallPluginIsWrittenVerbatim(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ok.lua")
	content := []byte("PLUGIN = { name = \"ok\" }\n")

	if err := downloadPluginBody(dest, bytes.NewReader(content)); err != nil {
		t.Fatalf("downloadPluginBody(small) = %v, want nil", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("落盘内容 = %q, want %q", got, content)
	}
}

// flakyReader 依次返回：数据 → io.EOF（拷贝正常收尾）→ 非 EOF 错误。
// 用于覆盖「探测阶段读失败」分支：不是超限，必须报读错误而不是假装成功。
type flakyReader struct {
	data []byte
	step int
}

func (r *flakyReader) Read(p []byte) (int, error) {
	switch r.step {
	case 0:
		r.step++
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	case 1:
		r.step++
		return 0, io.EOF
	default:
		return 0, errors.New("connection reset")
	}
}

// TestDownloadPluginBody_ProbeReadErrorIsReported 探测阶段的非 EOF 读错误
// 不能被当成「已到文件末尾」吞掉。
func TestDownloadPluginBody_ProbeReadErrorIsReported(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "flaky.lua")

	err := downloadPluginBody(dest, &flakyReader{data: []byte("PLUGIN = {}")})
	if err == nil {
		t.Fatal("downloadPluginBody(探测阶段读错误) = nil, want error")
	}
	if !strings.Contains(err.Error(), "read plugin body") {
		t.Errorf("错误文案未指出读取失败: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("读取失败后仍留下半截文件: stat err = %v", statErr)
	}
}
