//go:build windows

package env

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// documentsDir 返回 Windows「文档」文件夹的**真实**位置（Round 15）。
//
// 为什么不能用 `<USERPROFILE>\Documents`：该文件夹可以被重定向
// （OneDrive 已知文件夹备份、企业策略、非英文系统、手动改「位置」页），
// 此机上 `[Environment]::GetFolderPath('MyDocuments')` 是 `D:\documents`，
// 而 `C:\Users\<user>\Documents` 里那份 profile **根本不会被 PowerShell 加载**。
// 于是 `xdev init` / `xdev activate` 报 ✔、`doctor` 也报 ✔，但用户 shell 里
// XMDEV_HOME 与 cd hook 从未生效（实测：真实 $PROFILE 87 字节且无 xdev 块，
// 孤儿位置 118 字节且含 xdev 块）。
//
// 取值优先级与 Windows 一致：HKCU User Shell Folders\Personal
// （REG_EXPAND_SZ，registry 客户端会展开环境变量）→ 回退到
// `<USERPROFILE>\Documents`。
func documentsDir() string {
	const keyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`
	if k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE); err == nil {
		v, _, verr := k.GetStringValue("Personal")
		_ = k.Close()
		if verr == nil {
			if v = strings.TrimSpace(v); v != "" && filepath.IsAbs(v) {
				return filepath.Clean(v)
			}
		}
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Documents")
}
