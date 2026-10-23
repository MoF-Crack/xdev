//go:build windows

package shim

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobKillOnClose 把**当前进程**加入一个配置了
// `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的 Job Object，并返回 release。
//
// # 解决什么问题
//
// Windows 没有 execve 语义，shim 只能用 `cmd.Run()` 起子进程再等待
// （见 run_windows.go）。于是经 shim 启动的工具是一条**多一层**的进程树：
//
//	dsh / shell → xdev shim → 真实工具 → 它自己的子进程
//
// 上层应用（MCP client、LSP client、CI runner、`taskkill /PID`）只会杀掉
// **它自己的直接子进程**，也就是 shim。真实工具变成**孤儿**继续存活：
//   - 占着端口与管道；
//   - 等到控制台 Ctrl+C 或管道关闭时才死，把栈回溯**吐在提示符之后**；
//   - 上层无法可靠地停掉自己启动的服务。
//
// 实测（2026-10-23，`dsh` 的 godot-ai MCP 配置 `command: E:\XDev\shims\uvx.exe`）：
//
//	杀 shim(45188) 后：uvx.exe[10840] alive=True、uv.exe[28056] alive=True
//	→ Python MCP server 随后才被 KeyboardInterrupt 打断并打印 traceback，
//	  而此时 PowerShell 提示符已经返回。
//
// 加入 Job 后，shim 被杀死会带走整棵子树（含孙进程），上层即可可靠停止服务。
//
// # 为什么把**自己**加进去，而不是在启动后 assign 子进程
//
// `AssignProcessToJobObject` 只会把**那一个**进程加入 Job；子进程若在 assign
// 之前就已创建，就不会被纳入。`exec.Command` 的 Start 与 assign 之间存在竞态窗口。
// 而进程在创建时会**继承父进程的 Job 归属** —— 因此把自己加入 Job 之后，
// 之后 Start 出来的子进程自动属于该 Job，**没有竞态**，也不需要
// CREATE_SUSPENDED + 手工 ResumeThread（os/exec 不暴露线程句柄）。
//
// # 为什么正常退出时要**先清掉标志**（关键，避免引入新问题）
//
// 有些工具是**故意 daemon 化**的：`gradle --daemon` 的启动器退出后，守护进程
// 必须继续活着。如果 shim 正常退出时也让 KILL_ON_JOB_CLOSE 生效，就会误杀这些
// 守护进程 —— 那正是「修一个 bug 引入另一个 bug」。
//
// 因此语义分成两条：
//   - **正常退出**：release() 先清零 LimitFlags 再关句柄 → 孙进程照常存活；
//   - **异常终止**（被 kill / 崩溃）：Windows 关闭句柄时标志仍在 → 整棵树被
//     终止，不留孤儿。
//
// 返回 release 供调用方 `defer`，**必须在子进程结束后**调用（它会把 Job 的
// kill-on-close 关掉；在子进程运行期间调用会……其实只会关掉标志，不会杀进程，
// 但仍应在 cmd.Run() 之后调用以保持语义清晰）。
//
// ok=false 表示 Job 建立失败（例如宿主已把 shim 放进一个不允许嵌套的 Job）。
// 这时**保持旧行为**，只记日志 —— 进程管理绝不能因为 Job 设置失败而拒绝执行工具。
func jobKillOnClose() (release func(), ok bool) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("[shim] WARNING: CreateJobObject failed (%v) — child will not be reaped with the shim", err)
		return func() {}, false
	}

	if err := applyJobKillOnClose(job, true); err != nil {
		_ = windows.CloseHandle(job)
		log.Printf("[shim] WARNING: SetInformationJobObject failed (%v) — child will not be reaped with the shim", err)
		return func() {}, false
	}

	self, err := windows.GetCurrentProcess()
	if err != nil {
		_ = windows.CloseHandle(job)
		log.Printf("[shim] WARNING: GetCurrentProcess failed (%v)", err)
		return func() {}, false
	}
	if err := windows.AssignProcessToJobObject(job, self); err != nil {
		// 宿主可能已把 shim 放进一个不允许嵌套的 Job。保持旧行为，不阻断执行。
		_ = windows.CloseHandle(job)
		log.Printf("[shim] WARNING: AssignProcessToJobObject failed (%v) — child will not be reaped with the shim", err)
		return func() {}, false
	}

	return func() {
		// 先清掉 kill-on-close，让**正常退出**不连坐 daemon 化的孙进程
		// （`gradle --daemon` 等）。清失败也继续关句柄。
		if cerr := applyJobKillOnClose(job, false); cerr != nil {
			log.Printf("[shim] WARNING: could not clear job kill-on-close (%v)", cerr)
		}
		_ = windows.CloseHandle(job)
	}, true
}

// applyJobKillOnClose 在 job 上开启/关闭 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`。
//
// 拆成独立函数是为了**可测**：`release()` 会关闭句柄，句柄一关就无法再查询；
// 而「开启 → 查询确认 → 关闭 → 查询确认为 0」这条往返正是证明
// **正常退出时确实不会连坐**的唯一确定性手段 —— 这是本修复最关键、也最容易
// 写错的一步（写错就会误杀 `gradle --daemon` 这类守护进程）。
func applyJobKillOnClose(job windows.Handle, enabled bool) error {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if enabled {
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	}
	_, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	return err
}

// jobHasKillOnClose 查询 job 当前是否带 kill-on-close 标志（测试/诊断用）。
func jobHasKillOnClose(job windows.Handle) (bool, error) {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var ret uint32
	if err := windows.QueryInformationJobObject(
		job,
		int32(windows.JobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		&ret,
	); err != nil {
		return false, err
	}
	return info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE != 0, nil
}
