# 06 SwitchToLocalVersion 校验顺序 + 吞错

Status: resolved
Type: task
批次: B1
对应修复项: P0-08（方案 §4 P0-08）

## 问题

gui/app_env_vars.go:477-531：未校验 toolName 先拼 current/<toolName>（app.go:728 Join+Clean，`..\` 可破坏真实链接/逃出 current/）→ os.Remove + CreateJunction（:501-507，失败仅 logWarn）→ 之后才 mustGetPlugin（:509）→ :530 返回 nil。

## 方案

1. 函数开头依次 `util.ValidateShimName(toolName)`（sanitize.go:94-97）→ `a.mustGetPlugin(toolName)`，任一失败立即返回错误、不触碰文件系统。
2. :502 os.Remove 失败与 :505-507 CreateJunction 失败均返回错误（v1 只改 CreateJunction 分支——Remove 分支同样吞错）。
3. p.Shims() 循环的 CopyFile 失败保持 logWarn（非关键）。

## 守卫测试

①`SwitchToLocalVersion("..\\current\\node", tmpDir)` 报错且真实 current/node 未触碰；②未注册工具名报错无 FS 副作用；③junction 创建失败（只读目录模拟）返回错误。

## Comments（Batch 1 实施记录）

- 改动：`internal/gui/app_env_vars.go` SwitchToLocalVersion — 函数开头先 `util.ValidateShimName(toolName)` 再 `a.mustGetPlugin(toolName)`（任一失败零 FS 副作用；plugin 提前取也驱动后续 shim 刷新）；os.Remove 失败（非 IsNotExist）返回错误；CreateJunction 失败返回错误（原 "falling back" 日志是谎言——并无回退，且返回 nil 掩盖链接悬空）；p.Shims() 循环 CopyFile 失败保持 logWarn（非关键）。
- 守卫测试：`internal/gui/switch_local_test.go` — TestSwitchToLocalVersion_InvalidName_RejectsBeforeFS（L1：穿越名被拒且真实 current/node junction 原目标不变）/ TestSwitchToLocalVersion_Unregistered_RejectsBeforeFS（L2：current/ 列表逐项不变）/ TestSwitchToLocalVersion_JunctionFails_ReturnsError（L3：current/ 父目录缺失确定性注入失败，先红后绿）/ TestSwitchToLocalVersion_Valid_Unchanged（L4：回归）。
- 实施发现：Go 1.26 Windows 上 `filepath.EvalSymlinks` **不解析目录 junction**（返回原路径），`os.Readlink` 才是正确探针——与生产 util.ActiveVersion 一致（实证于本机）。L3 用「父目录缺失」替代只读目录（Windows 目录只读属性不阻止创建/删除）。
