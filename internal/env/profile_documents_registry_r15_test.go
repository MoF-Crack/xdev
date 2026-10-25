//go:build windows

package env

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registeredDocumentsForTest 读取 Windows 登记的「文档」位置（测试专用）。
// 与 documentsDir 的取值来源一致，用于「判据 vs 真实语料」的对照。
func registeredDocumentsForTest() string {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Personal")
	if err != nil {
		return ""
	}
	v = strings.TrimSpace(v)
	if v == "" || !filepath.IsAbs(v) {
		return ""
	}
	return filepath.Clean(v)
}
