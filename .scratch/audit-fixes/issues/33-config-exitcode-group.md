# 33 配置枚举/退出码组（P1-27/28/29）

Status: resolved（含 doctor symlink 提示未做的披露）
Type: task
批次: B5
对应修复项: P1-27(E-08) + P1-28(E-09) + P1-29(E-10)（方案 §5 主题 D）

## 方案要点

1. **P1-27 home 相对路径**：resolveXMDevDir/expandHome 返回前强制 `filepath.Abs`；`config set core.xmdevenv_dir` 与 drive_prompt "p" 输入 IsAbs 校验（拒绝相对；UNC 显式拒绝提示）。
2. **P1-28 path.mode 三套枚举收敛**：setPathField（cli/config.go:233-237）case 改 `shim|direct|auto`（拒绝 symlink——全仓仅 :234 一处定义）；SetPathMode（config.go:259-262）同步；存量 "symlink" 由 GetPathMode 降级 shim（现状）+ doctor 提示。
3. **P1-29 isolate 退出码**：main.go Execute 错误出口加 `var ece *shim.ExitCodeError; if errors.As(err, &ece) { os.Exit(ece.Code) }`（对照 :54-56 shim 分发路径先例）。

## 守卫测试

相对/UNC home 拒绝且绝对路径用户零影响；`config set path.mode symlink` 拒绝 / `auto` 接受；isolate 子进程非零退出码原样传播。

## Comments（Batch 5 实施记录）

- 改动：
  - **E-08**（`internal/config/config.go`）— `resolveXMDevDir` 拆为 wrapper（返回前 `filepath.Abs`）+ `resolveXMDevDirRaw`；`expandHome` 归一绝对（空串仍返回空，不退化成 CWD）。`internal/cli/config.go` 的 `config set core.xmdevenv_dir` 与 `internal/cli/drive_prompt.go` 向导 "p" 分支走新 `validateLocalAbsPath`（拒相对、拒 UNC，含 Windows `C:foo`、`\foo`、`\\srv\share`、`//srv/share`，空值拒绝；拒绝后重新提示）。
  - **E-09**（`internal/cli/config.go` + `internal/config/config.go`）— `setPathField mode` 收为 `shim|direct|auto`（拒绝 `symlink`，报错文案同步）；`SetPathMode` 注释声明三值同源；存量 `symlink` 由 `GetPathMode` 静默降级（现状保持）。
  - **E-10**（`main.go`）— 新增 `exitCodeForError` + `errors.As` 传播 `*shim.ExitCodeError`（对照既有 shim 分发路径先例，含 Code==0 防御）。
- 守卫测试：`internal/cli/config_path_guard_test.go`（拒相对/UNC ×2 处 + `path.mode symlink` 拒 / `auto` 接受）、`internal/config/abs_path_test.go`（`TestLoad_RelativeHomeBecomesAbsolute` / `TestResolveXMDevDir_AlwaysAbsolute` / `TestExpandHome_EmptyStaysEmpty` / `TestLoadDerivedPathsAreAbsolute`）、根包 `main_exitcode_test.go`（映射 + **真子进程**传播）、`internal/cli/isolate_exitcode_test.go`（**真子进程**：helper 立即 exit 7 → `isolate --mode none exec --` 断言 `*shim.ExitCodeError.Code == 7`）。
- **红线先红后绿**：去掉校验 → 相对/UNC/空值被接受 + `setPathField(mode,"symlink") = nil, want rejection` + `auto` 被拒；`exitCodeForError` 改恒 1 → `child exit code = 99, want 7（修复前恒 1）`（5 个映射子例全红）。还原后全绿，无探针残留。
- **未做项（披露）**：票面要求的 **doctor 对存量 `path.mode: symlink` 的提示**未实现——`internal/cli/doctor.go` 不在该 subagent 白名单（该文件被票 40 占用）。现状：`GetPathMode` 静默降级为 shim。建议后续小票补提示。
- 白名单外发现（只报告）：`internal/cli/init.go` 向导 Step 3 只判 `!NoPathFix()`，**未判 `cfg.Path.InjectToShellRC`** → 配置里关掉 shell RC 注入对 `xdev init` 无效（超出票面，未改）。
