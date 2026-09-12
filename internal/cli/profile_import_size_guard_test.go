package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 40（P2-33 / N-18）：CLI profile import 的 10 MB 上限
// ---------------------------------------------------------------------------
//
// GUI 侧 ImportProfile 有 profileMaxSize（10 MB）上限，CLI 侧原先直接
// os.ReadFile 任意大小文件 —— 误选/恶意构造的超大 JSON 会直接把 CLI
// 读爆内存。此处对齐同一上限，并且不做「截断后解析」：截断只会把
// 「文件过大」伪装成「JSON 解析失败」。

// writeSparseFile 生成指定大小的文件（用 Truncate 造稀疏文件，避免真的
// 写 10 MB 数据）。
func writeSparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatalf("truncate %s: %v", path, err)
	}
}

// TestReadProfileForImport_RejectsOversizeFile 超过 10 MB → 明确报错，
// 错误文案指出「过大」而不是解析失败。
func TestReadProfileForImport_RejectsOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge-profile.json")
	writeSparseFile(t, path, profileImportMaxSize+1)

	data, err := readProfileForImport(path)
	if err == nil {
		t.Fatalf("readProfileForImport(%d bytes) = nil, want over-limit error", profileImportMaxSize+1)
	}
	if data != nil {
		t.Errorf("超限文件仍然返回了 %d 字节内容", len(data))
	}
	if !strings.Contains(err.Error(), "too large") || !strings.Contains(err.Error(), "10 MiB") {
		t.Errorf("错误文案未说明大小上限: %v", err)
	}
}

// TestReadProfileForImport_AcceptsFileAtLimit 恰好 10 MB 允许（边界值）。
func TestReadProfileForImport_AcceptsFileAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "at-limit.json")
	writeSparseFile(t, path, profileImportMaxSize)

	data, err := readProfileForImport(path)
	if err != nil {
		t.Fatalf("readProfileForImport(恰好 10 MB) = %v, want nil", err)
	}
	if int64(len(data)) != profileImportMaxSize {
		t.Errorf("读取长度 = %d, want %d", len(data), profileImportMaxSize)
	}
}

// TestReadProfileForImport_ReadsNormalProfile 正常大小文件原样读取，
// 且缺失文件的错误文案保持「read file: ...」（既有行为）。
func TestReadProfileForImport_ReadsNormalProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	content := []byte(`{"version":"1.0.0","pathMode":"shim"}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	data, err := readProfileForImport(path)
	if err != nil {
		t.Fatalf("readProfileForImport(normal) = %v, want nil", err)
	}
	if string(data) != string(content) {
		t.Errorf("读取内容 = %q, want %q", data, content)
	}

	if _, err := readProfileForImport(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("readProfileForImport(缺失文件) = nil, want error")
	} else if !strings.Contains(err.Error(), "read file") {
		t.Errorf("缺失文件的错误文案变化: %v", err)
	}
}
