//go:build windows

package shim

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 2026-10-23（进程树收口）：验证 jobKillOnClose 真的把整棵子树连坐。
//
// # 缺陷现场（实测）
//
// Windows 无 execve，shim 只能 `cmd.Run()` 起子进程。于是经 shim 启动的工具是
// 一条多一层的进程树，而上层应用（MCP client / LSP client / `taskkill /PID`）
// 只杀**它的直接子进程**（shim）。实测（dsh 的 godot-ai MCP，command 指向
// `E:\XDev\shims\uvx.exe`）：
//
//	杀 shim(45188) → uvx.exe[10840] alive=True、uv.exe[28056] alive=True
//
// 真实 server 变孤儿，占着端口；直到控制台 Ctrl+C 才被 KeyboardInterrupt 打断，
// 把 Python 栈回溯**打印在 PowerShell 提示符之后**（这正是用户看到的症状）。
//
// # 本守卫的两条方向（缺一不可）
//
//  1. **有 Job**：杀 shim → 孙进程必须**消失**；
//  2. **无 Job（对照）**：同样的杀法 → 孙进程必须**存活**。
//
// 只测第 1 条是不够的：如果孙进程本来就因为别的原因（例如它自己退出、或参数
// 写错根本没起来）而不在，第 1 条会假绿。对照组证明「孙进程确实能活过父进程之死」，
// 从而证明第 1 条的绿是 Job 带来的。
const (
	jobProbeEnv  = "XDEV_JOB_PROBE"      // ""=编排者 | "helper" | "grandchild"
	jobModeEnv   = "XDEV_JOB_PROBE_MODE" // "with-job" | "no-job"
	jobDirEnv    = "XDEV_JOB_PROBE_DIR"
	jobReadyName = "helper-ready"
	jobPIDName   = "grandchild-pid"
)

func TestJobKillOnClose_ReapsWholeTree(t *testing.T) {
	// --- 被 re-exec 出来的角色 ---
	switch os.Getenv(jobProbeEnv) {
	case "helper":
		jobHelperMain(t)
		return
	case "grandchild":
		jobGrandchildMain()
		return
	}

	// --- 编排者 ---
	dir := t.TempDir()

	for _, mode := range []string{"with-job", "no-job"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			gcPID := runJobScenario(t, dir, mode)
			defer killPID(gcPID) // 无论如何都清理，别留孤儿
			_ = gcPID
		})
	}
}

// runJobScenario 起 helper（→ grandchild），杀掉 helper，然后断言孙进程的生死。
// 返回孙进程 pid（供调用方兜底清理）。
func runJobScenario(t *testing.T, dir, mode string) int {
	t.Helper()

	// 每个 scenario 用独立子目录，避免 pid 文件互相覆盖。
	sub := filepath.Join(dir, mode)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	cmd := exec.Command(self, "-test.run=^TestJobKillOnClose_ReapsWholeTree$")
	cmd.Env = append(os.Environ(),
		jobProbeEnv+"=helper",
		jobModeEnv+"="+mode,
		jobDirEnv+"="+sub,
	)
	// helper 不应继承 XDEV_SHIM_CHILD（那会让 TestMain 直接退出）。
	cmd.Env = withoutEnv(cmd.Env, "XDEV_SHIM_CHILD")

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	helperPID := cmd.Process.Pid
	defer killPID(helperPID)

	// 等 helper 报告孙进程已起来。
	ready := filepath.Join(sub, jobReadyName)
	if !waitForFile(ready, 30*time.Second) {
		t.Fatalf("helper 未在 30s 内就绪（mode=%s）—— 探针本身失败，不是被测行为", mode)
	}
	rawPID, err := os.ReadFile(filepath.Join(sub, jobPIDName))
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	gcPID, err := strconv.Atoi(string(trimSpaceBytes(rawPID)))
	if err != nil || gcPID <= 0 {
		t.Fatalf("bad grandchild pid %q: %v", rawPID, err)
	}

	// 前置断言：孙进程此刻必须真的活着（否则下面的「消失」毫无意义）。
	if !processAlive(gcPID) {
		t.Fatalf("前置条件失败：孙进程 %d 在杀父之前就已不在了", gcPID)
	}

	// **模拟上层应用**：只杀它的直接子进程（helper/shim）。
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = cmd.Wait()

	gone := waitForProcessGone(gcPID, 15*time.Second)

	switch mode {
	case "with-job":
		if !gone {
			t.Errorf("Job Object 未生效：杀掉 shim 后孙进程 %d **仍然存活** —— "+
				"真实工具会变孤儿、占着端口，并在提示符之后才吐栈回溯", gcPID)
		}
	case "no-job":
		if gone {
			t.Errorf("对照组异常：**没有** Job 时孙进程 %d 竟然也随父进程消失了 —— "+
				"这说明本用例测不出 Job 的作用（可能在别的环节被连坐）", gcPID)
		}
	default:
		t.Fatalf("unknown mode %q", mode)
	}
	return gcPID
}

// jobHelperMain 扮演 shim：可选地建立 kill-on-close Job，然后起孙进程并常驻。
func jobHelperMain(t *testing.T) {
	dir := os.Getenv(jobDirEnv)
	mode := os.Getenv(jobModeEnv)

	if mode == "with-job" {
		// 关键：**不要**调用 release —— 本 helper 稍后会被外部 Kill，
		// 模拟 shim 被 taskkill；此时 Job 的 kill-on-close 仍生效。
		if _, ok := jobKillOnClose(); !ok {
			fmt.Fprintln(os.Stderr, "JOB-PROBE: jobKillOnClose returned ok=false")
			os.Exit(3)
		}
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "JOB-PROBE: executable:", err)
		os.Exit(3)
	}
	gc := exec.Command(self, "-test.run=^TestJobKillOnClose_ReapsWholeTree$")
	gc.Env = append(withoutEnv(os.Environ(), "XDEV_SHIM_CHILD"),
		jobProbeEnv+"=grandchild",
		jobDirEnv+"="+dir,
	)
	if err := gc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "JOB-PROBE: start grandchild:", err)
		os.Exit(3)
	}
	if err := os.WriteFile(filepath.Join(dir, jobPIDName),
		[]byte(strconv.Itoa(gc.Process.Pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "JOB-PROBE: write pid:", err)
		os.Exit(3)
	}
	if err := os.WriteFile(filepath.Join(dir, jobReadyName), []byte("ok"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "JOB-PROBE: write ready:", err)
		os.Exit(3)
	}

	// 常驻，等待被外部 Kill。
	time.Sleep(120 * time.Second)
}

// jobGrandchildMain 扮演真实工具（例如 Python MCP server）：常驻直到被杀。
func jobGrandchildMain() {
	dir := os.Getenv(jobDirEnv)
	_ = os.WriteFile(filepath.Join(dir, "grandchild-alive"), []byte("ok"), 0o644)
	time.Sleep(120 * time.Second)
}

// ---- helpers ----

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0:0]
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			continue
		}
		out = append(out, e)
	}
	return out
}

func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\n' || b[start] == '\r' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\r' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// processAlive 报告 pid 是否仍是活着的进程。
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259 // STILL_ACTIVE
	return code == stillActive
}

func waitForProcessGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !processAlive(pid)
}

func killPID(pid int) {
	if pid <= 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
}

// TestRunWindows_WiresJobBeforeStart 是**接线守卫**：证明 `run_windows.go` 真的
// 用上了 jobKillOnClose，而不只是「函数存在、无人调用」。
//
// 只测 jobKillOnClose 自身是不够的 —— 那可以绿着而生产路径根本没接上
// （本仓库历史上「机制存在但没接线」出现过多次）。这里钉住两点：
//  1. `jobKillOnClose()` 必须在 `cmd.Run()` **之前**调用（子进程创建时继承 Job）；
//  2. 必须有 `defer release()`（否则正常退出时不清标志，会误杀 daemon 化孙进程）。
func TestRunWindows_WiresJobBeforeStart(t *testing.T) {
	data, err := os.ReadFile("run_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	// **必须剥掉注释**：本文件的说明注释里为了解释语义恰恰写着 `cmd.Run()`
	// 与 `release()`（本守卫初版就因此对自己误报，判据命中了第 31 行的注释）。
	// 这是本仓库源码级守卫的第四次同类误报 —— 凡是比较「位置/出现次数」的
	// 源码守卫，一律先剥注释。
	var code strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	exec := code.String()

	iJob := strings.Index(exec, "jobKillOnClose()")
	iRun := strings.Index(exec, "cmd.Run()")
	if iJob < 0 {
		t.Fatal("run_windows.go 未调用 jobKillOnClose —— Job Object 没有接线")
	}
	if iRun < 0 {
		t.Fatal("run_windows.go 未找到 cmd.Run()")
	}
	if iJob > iRun {
		t.Error("jobKillOnClose() 必须在 cmd.Run() **之前**调用 —— " +
			"子进程只有在创建时才会继承 Job 归属；Start 之后再 assign 存在竞态")
	}
	if !strings.Contains(exec, "defer release()") {
		t.Error("必须 defer release() —— 正常退出时要先清掉 kill-on-close，" +
			"否则 `gradle --daemon` 这类故意 daemon 化的孙进程会被误杀")
	}
}

// TestApplyJobKillOnClose_RoundTrips 用**查询**确定性证明「正常退出不连坐」。
//
// 为什么不用「起一个 daemon、让 shim 正常退出、看它是否存活」来测：本机实测
// 该做法**测不出真东西** —— 绕过 shim 直接跑真 python 时，daemon 孙进程同样
// 在命令结束时消失（harness 会在命令收尾时回收控制台进程组）。也就是说那种
// 断言会因为与被测行为无关的原因而红/绿。
//
// 改为直接查询 Job 的 LimitFlags —— 这是唯一不依赖进程存活时机的确定性判据：
//
//	开启 → 查询 = true       （异常终止会连坐）
//	关闭 → 查询 = false      （正常退出不连坐 ← 保护 `gradle --daemon`）
//
// 若把 release() 里的清零写错（漏掉、或用错 InfoClass），第二条断言立刻红。
func TestApplyJobKillOnClose_RoundTrips(t *testing.T) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatalf("CreateJobObject: %v", err)
	}
	// 本用例刻意**不**把自己加入该 Job：关句柄时若标志仍开着，
	// 会去终止 Job 成员；没有成员则安全无副作用。
	defer windows.CloseHandle(job)

	if err := applyJobKillOnClose(job, true); err != nil {
		t.Fatalf("apply(true): %v", err)
	}
	on, err := jobHasKillOnClose(job)
	if err != nil {
		t.Fatalf("query after enable: %v", err)
	}
	if !on {
		t.Fatal("开启后 LimitFlags 应含 KILL_ON_JOB_CLOSE —— 否则异常终止不会连坐，修复无效")
	}

	if err := applyJobKillOnClose(job, false); err != nil {
		t.Fatalf("apply(false): %v", err)
	}
	off, err := jobHasKillOnClose(job)
	if err != nil {
		t.Fatalf("query after disable: %v", err)
	}
	if off {
		t.Error("关闭后 LimitFlags 仍含 KILL_ON_JOB_CLOSE —— 正常退出会连坐，" +
			"`gradle --daemon` 这类故意 daemon 化的孙进程会被误杀（本修复引入的新问题）")
	}
}
