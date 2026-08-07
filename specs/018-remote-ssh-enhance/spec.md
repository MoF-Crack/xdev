# Spec 018: 远程开发 SSH 增强

> **状态**：待用户批准  
> **类型**：功能增强  
> **日期**：2026-08-01

## 问题

远程开发页面缺少：
1. SSH 密钥路径无文件浏览按钮
2. 无密码登录支持
3. 无连接测试功能

## 现状

| 组件 | 现状 |
|------|------|
| `RemoteDevelopmentPage.vue` | 有 host/port/user/keyPath 输入，无浏览/密码/测试 |
| `SSHTarget` | 无 `Password` 字段 |
| `SSHAdapter` | 用 `ssh -i keyPath`，无密码支持 |
| `app_project.go` | `CreateRemoteInstance` 会 `ssh echo ok` 测试，但无独立 `TestConnection` |
| `BrowseDirectory` | 有目录选择对话框，但无文件选择对话框 |

## 修复

### F1: 密钥路径文件浏览

后端 `app_project.go` 新增 `BrowseFile(title, filters string) string`：
- 用 `wailsRuntime.OpenFileDialog`
- filters 支持 `.pem,.key,.id_rsa` 等

前端 `RemoteDevelopmentPage.vue`：
- keyPath 输入旁加「浏览」按钮
- 调 `window.go.gui.App.BrowseFile(t('remote.select_key'), '*.pem;*.key;*.id_rsa;*.id_ed25519;*.id_ecdsa;*.id_dsa')`

### F2: 密码登录

`SSHTarget` 加 `Password string`。
`SSHAdapter.sshArgs()` 加密码支持：
- 密码非空：用 `sshpass -p password ssh ...`（需 sshpass 安装；Windows 上用 `%USERPROFILE%\.ssh\config` 或提示用户用密钥）
- 密码为空：走密钥认证（现有行为）

前端：SSH 模式下加 `password` 输入框（`type="password"`）。

### F3: 连接测试

后端 `app_project.go` 新增 `TestSSHConnection(host, port, user, keyPath, password string) *TestResult`：
- 构建 SSHAdapter，执行 `ssh echo ok`
- 返回 `{ success: bool, message: string, latencyMs: int }`

前端：SSH 模式下加「测试连接」按钮，调 `TestSSHConnection`，显示结果。

### F4: 禁用多余功能

不影响 Docker 模式。

## 验证

1. SSH 模式下浏览密钥文件 → 路径填入
2. 输入密码 → 测试连接 → 显示延迟
3. 无密码 + 密钥 → 测试连接 → 成功
4. 错误连接 → 测试连接 → 显示错误消息
5. `go test ./internal/orchestrator/`
6. `wails build`

## 批准记录 (Approval History)

> **批准ID**: `2026-08-08-spec-018-draft-001`
