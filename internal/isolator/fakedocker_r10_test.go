// fakedocker_r10_test.go 提供第十轮复现用例共用的「假 docker」接缝。
//
// 本机与 CI 都没有 docker 守护进程，而 P2-2（saveState 失败必须回滚容器）、
// P2-3（Teardown 必须按**持久化会话**而不是构造时快照删容器）、P3-2
// （"No such container" 必须视为成功）三条缺陷都发生在 docker 调用边界上。
//
// 仓库既有测试接缝只有「纯文件操作」的 generateFlake / generateDevcontainer，
// 覆盖不到 docker 调用段。这里新增最小接缝：把一个假 docker 可执行文件前置到
// PATH。生产代码走的是 exec.LookPath("docker") + exec.Command("docker", ...)
// （container.go:38/42/128/174），因此**不需要改动生产代码**就能精确控制
// run/rm 的输出与退出码（Windows 用 docker.cmd，POSIX 用 sh shim）。
package isolator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeContainerID 是假 docker 为 `docker run` 打印的容器 ID。P2-2 / P2-3 / P3-2
// 的用例都靠它断言「这次 Teardown/回滚到底删了哪个容器」。
const fakeContainerID = "fakedocker0123456789abcdef"

// fakeDockerBehavior 控制假 docker 的 `rm` 行为。
type fakeDockerBehavior int

const (
	// fakeDockerOK：run 打印容器 ID，rm 成功。
	fakeDockerOK fakeDockerBehavior = iota
	// fakeDockerRMNoSuchContainer：rm 以 1 退出并报 "No such container"
	//（容器已被用户手工 `docker rm` 的现场）。
	fakeDockerRMNoSuchContainer
	// fakeDockerRMFails：rm 以 1 退出但**不是** "No such container"，
	// 用于守卫「不得把所有 rm 失败都当成功」。
	fakeDockerRMFails
)

// fakeDocker 是一个已安装到 PATH 的假 docker。
type fakeDocker struct {
	dir     string
	logPath string
}

// newFakeDocker 写入假 docker 并把其目录前置到 PATH。用例结束后 PATH 由
// t.Setenv 自动还原。
func newFakeDocker(t *testing.T, behavior fakeDockerBehavior) *fakeDocker {
	t.Helper()
	return newFakeDockerWithRunHook(t, behavior, "")
}

// newFakeDockerWithRunHook 额外在 `docker run` 分支里执行 runHook（平台相关文本，
// 由调用方给出）。这是唯一能在「容器已启动、state 尚未落盘」这个窗口里注入故障的
// 接缝：真实的磁盘满 / 权限变更正是发生在这一刻。
func newFakeDockerWithRunHook(t *testing.T, behavior fakeDockerBehavior, runHook string) *fakeDocker {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker-invocations.log")
	idPath := filepath.Join(dir, "fake-container-id.txt")

	name, body := "docker", posixFakeDockerScript(logPath, idPath, behavior, runHook)
	if runtime.GOOS == "windows" {
		name, body = "docker.cmd", windowsFakeDockerScript(logPath, idPath, behavior, runHook)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &fakeDocker{dir: dir, logPath: logPath}
}

// invocations 返回假 docker 收到的全部命令行（按调用顺序）。
func (f *fakeDocker) invocations(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// rmArgs 只返回 `rm` 调用行，例如 "rm -f fakedocker0123456789abcdef"。
func (f *fakeDocker) rmArgs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, line := range f.invocations(t) {
		if strings.HasPrefix(line, "rm ") {
			out = append(out, line)
		}
	}
	return out
}

// windowsFakeDockerScript 生成 docker.cmd。脚本不用括号块包住整段逻辑，而是用
// goto 标签，避免 cmd.exe 在括号块里对重定向/标签的解析歧义。
func windowsFakeDockerScript(logPath, idPath string, behavior fakeDockerBehavior, runHook string) string {
	rmBody := "exit /b 0\r\n"
	switch behavior {
	case fakeDockerRMNoSuchContainer:
		rmBody = "echo Error response from daemon: No such container: %3 1>&2\r\nexit /b 1\r\n"
	case fakeDockerRMFails:
		rmBody = "echo Error response from daemon: permission denied 1>&2\r\nexit /b 1\r\n"
	}
	hook := ""
	if runHook != "" {
		hook = runHook + "\r\n"
	}
	return "@echo off\r\n" +
		"echo %*>>\"" + logPath + "\"\r\n" +
		"if \"%1\"==\"info\" exit /b 0\r\n" +
		"if \"%1\"==\"run\" goto xdevfakedockerrun\r\n" +
		"if \"%1\"==\"rm\" goto xdevfakedockerrm\r\n" +
		"exit /b 0\r\n" +
		":xdevfakedockerrun\r\n" +
		"if not exist \"" + idPath + "\" (>\"" + idPath + "\" echo " + fakeContainerID + ")\r\n" +
		"type \"" + idPath + "\"\r\n" +
		hook +
		"exit /b 0\r\n" +
		":xdevfakedockerrm\r\n" +
		rmBody
}

// posixFakeDockerScript 生成 POSIX sh 版假 docker（CI 在 Linux 上跑）。
func posixFakeDockerScript(logPath, idPath string, behavior fakeDockerBehavior, runHook string) string {
	rmBody := "exit 0"
	switch behavior {
	case fakeDockerRMNoSuchContainer:
		rmBody = "echo \"Error response from daemon: No such container: $3\" >&2\nexit 1"
	case fakeDockerRMFails:
		rmBody = "echo \"Error response from daemon: permission denied\" >&2\nexit 1"
	}
	hook := ""
	if runHook != "" {
		hook = "    " + runHook + "\n"
	}
	return "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"case \"$1\" in\n" +
		"  info) exit 0 ;;\n" +
		"  run)\n" +
		"    [ -f \"" + idPath + "\" ] || printf '%s\\n' \"" + fakeContainerID + "\" > \"" + idPath + "\"\n" +
		"    cat \"" + idPath + "\"\n" +
		hook +
		"    exit 0 ;;\n" +
		"  rm)\n" +
		"    " + rmBody + " ;;\n" +
		"esac\n" +
		"exit 0\n"
}

// blockStateDir 把 <home>/isolate 造成**文件**，使 saveState 的 MkdirAll 必然
// 失败（P2-2 审计现场手法：`nix setup err=<nil> state_exists=false`）。
func blockStateDir(t *testing.T, home string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "isolate"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// blockStateFileAsDir 把 state 文件路径本身造成**目录**，使 saveState 的原子
// rename 必然失败（第二个被吞掉的失败点，persist.go:102-104）。
func blockStateFileAsDir(t *testing.T, home, mode string) {
	t.Helper()
	if err := os.MkdirAll(statePath(home, mode), 0o755); err != nil {
		t.Fatal(err)
	}
}

// blockStateDirAfterRun 返回一段假 docker 脚本片段：在 `docker run` **成功打印
// 容器 ID 之后**把 <home>/isolate 造成文件，使随后的 saveState MkdirAll 失败。
//
// 为什么必须在 run 之后注入：这是唯一「容器已经真的在运行、state 却写不下去」的
// 窗口——真实世界里的磁盘满 / 权限在运行中被改正是发生在此刻。若在 run 之前注入，
// Setup 会先 loadState 发现损坏并 quarantine 腾空（A-02 语义），随后的 saveState
// 反而成功，覆盖不到回滚逻辑。
//
// 注意：quarantineState 对「父目录是文件」的状态是安全的无操作（os.Stat 失败 →
// 原样留下并返回说明），因此这个注入不会被 A-02 的修复顺手化解。
func blockStateDirAfterRun(t *testing.T, home string) string {
	t.Helper()
	p := filepath.Join(home, "isolate")
	if runtime.GOOS == "windows" {
		// rmdir 对不存在/非空目录失败不影响后续 mkdir 的语义；用 2>nul 静默。
		return "rmdir \"" + p + "\" 2>nul\r\necho not a directory> \"" + p + "\""
	}
	return "rm -rf \"" + p + "\" && printf 'not a directory\\n' > \"" + p + "\""
}
