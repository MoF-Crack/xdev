//go:build windows

// userenv_hermetic_test.go — internal/env 整包「宿主零接触」闸门（第四轮复核产物）。
//
// 背景（P0-17 事故复盘的最终结论）：逐个用例做 save/restore 挡不住事故 —— 进程被
// 强杀、panic、断言提前退出时恢复代码不会执行，测试值就**永久留在真实
// HKCU\Environment**；并发跑全量时，别的包的宿主机快照窗口还会撞上瞬态写入产生
// 假红。本文件把防护从「用例自律」升级为「包级不变量」：
//
//   - TestMain 为整个测试二进制安装内存 PATH 后端 + 内存用户环境后端（种子 = 启动
//     时的真实注册表快照），一切用例（含未来新增的）从结构上就写不到真实注册表；
//   - 每个真实注册表写/删函数入口处有 realUserRegistryMutationCount 埋点（见
//     win_path_windows.go），TestMain 在跑完全部用例后断言 计数增量 == 守卫声明值
//     （expectedRealRegistryMutationCount）—— 任何未声明的真实写/删尝试都让整包
//     失败，无论它发生在哪个用例、是否又「恢复」了；
//   - 守卫用例本身用「先于任何落盘即失败」的零变更尝试自证埋点有效（M-2 长度拒绝、
//     UTF16 编码失败、删除确认不存在的值），因此闸门全程不触碰宿主。
//
// 逃生口：XMDEV_ALLOW_REAL_REGISTRY_TESTS=1 时 TestMain 停用内存后端与计数断言，
// 供未来需要真实注册表的人工集成测试使用 —— 勿写入 CI 或常规 `go test`。
package env

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// TestMain 是 internal/env 测试二进制的宿主安全闸门（全包唯一 TestMain）。
func TestMain(m *testing.M) {
	if os.Getenv("XMDEV_ALLOW_REAL_REGISTRY_TESTS") != "" {
		fmt.Fprintln(os.Stderr, "[env-test] XMDEV_ALLOW_REAL_REGISTRY_TESTS 已设置 — 以真实注册表后端运行（人工集成模式，防污染闸门停用）")
		os.Exit(m.Run())
	}

	// 种子：只读真实注册表。内存后端必须先于 m.Run() 安装 —— 计数基线也要在
	// 任何用例之前捕获。
	realPath, _ := readUserPathFromRegistry()
	beforeEnv := enumerateUserEnvVarsFromRegistry()
	baseline := realUserRegistryMutationCount.Load()

	_, restorePathBackend := UseInMemoryUserPathForTesting(realPath)
	restoreEnvBackend := UseInMemoryUserEnvForTesting(beforeEnv)

	code := m.Run()

	restorePathBackend()
	restoreEnvBackend()

	// 闸门断言：真实注册表写/删尝试数 == 守卫声明的合法零落盘尝试数。
	delta := realUserRegistryMutationCount.Load() - baseline
	declared := expectedRealRegistryMutationCount.Load()
	if delta != declared {
		fmt.Fprintf(os.Stderr,
			"[env-test] HOST-SAFETY VIOLATION: 真实注册表写/删尝试 %d 次（守卫仅声明 %d 次）—— internal/env 的测试必须全程运行在内存后端上；未走接缝的真实写点请改经 userEnvWriteFn/userEnvDeleteFn/userPathWriteFn 或在 userenv_hermetic_test.go 声明\n",
			delta, declared)
		if code == 0 {
			code = 1
		}
	}

	// 复核（只提示不判死）：真实 HKCU 的差异也可能来自进程外的合法变更
	//（用户或其它程序），不属于测试污染；计数器才是判定依据。
	if after := enumerateUserEnvVarsFromRegistry(); !sameUserEnvMap(beforeEnv, after) {
		fmt.Fprintf(os.Stderr, "[env-test] NOTE: 真实 HKCU\\Environment 在本次运行期间发生变化（若非外部程序所为，请检查接缝泄漏）：%s\n",
			diffUserEnvMaps(beforeEnv, after))
	}

	os.Exit(code)
}

func sameUserEnvMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func diffUserEnvMaps(before, after map[string]string) string {
	names := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		names[k] = struct{}{}
	}
	for k := range after {
		names[k] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for k := range names {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var parts []string
	for _, k := range sorted {
		bv, bok := before[k]
		av, aok := after[k]
		switch {
		case bok && !aok:
			parts = append(parts, fmt.Sprintf("-%s=%q", k, bv))
		case !bok && aok:
			parts = append(parts, fmt.Sprintf("+%s=%q", k, av))
		case bok && aok && bv != av:
			parts = append(parts, fmt.Sprintf("~%s: %q → %q", k, bv, av))
		}
	}
	return strings.Join(parts, "; ")
}

// ---- 守卫 1：trusted 写/删必须经接缝，真实注册表零接触 ----
//
// 区分性（红探针已验证）：把 SetUserEnvVarTrusted 的落盘临时改回直调
// writeUserEnvVarToRegistry，本守卫立即红 —— 真实 HKCU 中出现探针值；
// DeleteUserEnvVarTrusted 直调真身时，删除落在真实注册表上而内存后端的
// 探针纹丝不动，同样红。两处红探针都由本文件的前置断言保护（真实注册表
// 不存在探针才继续），且 TestMain 的计数断言会同时告警。
func TestUserEnvSeam_WriteAndDeleteNeverTouchRealRegistry(t *testing.T) {
	// 自带内存后端：即使以 XMDEV_ALLOW_REAL_REGISTRY_TESTS 运行（TestMain 未装
	// 包级后端），本守卫也独立成立。
	restore := UseInMemoryUserEnvForTesting(nil)
	defer restore()

	const probeName = "XMDEV_SEAM_PROBE"
	const probeValue = `C:\seam-probe-value`

	if v, ok := enumerateUserEnvVarsFromRegistry()[probeName]; ok {
		t.Fatalf("前置条件失败：真实 HKCU 已存在 %s=%q（疑似历史红探针残留）— 请 reg delete 后重跑", probeName, v)
	}

	if err := SetUserEnvVarTrusted(probeName, probeValue); err != nil {
		t.Fatalf("SetUserEnvVarTrusted: %v", err)
	}
	got, err := GetUserEnvVarTrusted(probeName)
	if err != nil || got != probeValue {
		t.Fatalf("写入后经接缝应可见: got %q, err %v, want %q", got, err, probeValue)
	}
	if v, ok := enumerateUserEnvVarsFromRegistry()[probeName]; ok {
		t.Fatalf("接缝被绕过：真实 HKCU\\Environment 出现探针 %s=%q —— SetUserEnvVarTrusted 必须经 userEnvWriteFn 落地", probeName, v)
	}

	if err := DeleteUserEnvVarTrusted(probeName); err != nil {
		t.Fatalf("DeleteUserEnvVarTrusted: %v", err)
	}
	if _, err := GetUserEnvVarTrusted(probeName); err == nil {
		t.Fatal("删除后探针应从内存后端消失")
	}
	if _, ok := enumerateUserEnvVarsFromRegistry()[probeName]; ok {
		t.Fatalf("接缝被绕过：删除操作触碰了真实 HKCU（%s 仍存在）", probeName)
	}
}

// ---- 守卫 2：PATH 真身写函数的埋点先于任何注册表访问 ----
//
// 零落盘例证：M-2 长度校验在触碰注册表之前拒绝 40000 字符的 PATH —— 计数仍 +1。
// 区分性：删掉 writeUserPathFromRegistry 入口的埋点，本守卫立即红。
func TestUserPathSeam_RealWriterCountsAttemptBeforeRegistry(t *testing.T) {
	expectedRealRegistryMutationCount.Add(1) // 声明：本次运行 1 次合法的零落盘真实写尝试
	before := realUserRegistryMutationCount.Load()

	if err := writeUserPathFromRegistry(strings.Repeat("X", 40000)); err == nil {
		t.Fatal("M-2 长度校验必须拒绝超过 32767 字符的 PATH")
	}
	if delta := realUserRegistryMutationCount.Load() - before; delta != 1 {
		t.Fatalf("真实写函数入口必须先于注册表访问计数尝试: delta=%d, want 1", delta)
	}
}

// ---- 守卫 3：env 写/删真身的埋点先于任何落盘 ----
//
// 零落盘例证：写 —— 值含内嵌 NUL，UTF16 编码在任何 RegSetValueExW 之前失败；
// 删 —— 真实注册表确认不存在该值，RegDeleteValueW 返回 FILE_NOT_FOUND，无变更。
// 区分性：删掉任一入口埋点，本守卫的 delta 断言立即红。
func TestUserEnvSeam_RealWritersCountAttemptBeforeRegistry(t *testing.T) {
	expectedRealRegistryMutationCount.Add(2) // 1 次写尝试 + 1 次删尝试，均为零落盘
	before := realUserRegistryMutationCount.Load()

	if err := writeUserEnvVarToRegistry("XMDEV_PROBE_NAME", "a\x00b"); err == nil {
		t.Fatal("含内嵌 NUL 的值必须在 UTF16 编码阶段失败（先于任何写入）")
	}
	const absentProbe = "XMDEV_SEAM_PROBE_ABSENT"
	if v, ok := enumerateUserEnvVarsFromRegistry()[absentProbe]; ok {
		t.Fatalf("前置条件失败：真实 HKCU 已存在 %s=%q", absentProbe, v)
	}
	if err := deleteUserEnvVarFromRegistry(absentProbe); err != nil {
		t.Fatalf("删除确认不存在的值应为无变更成功: %v", err)
	}
	if v, ok := enumerateUserEnvVarsFromRegistry()[absentProbe]; ok {
		t.Fatalf("删除不存在的值不得创建/修改任何真实值: %s=%q", absentProbe, v)
	}

	if delta := realUserRegistryMutationCount.Load() - before; delta != 2 {
		t.Fatalf("真实写/删函数入口必须计数尝试: delta=%d, want 2", delta)
	}
}

// ---- 守卫 4：通用 SetEnvVar/DeleteEnvVar 的 user 级必须经接缝 ----
//
// 第四轮复核发现的闸门缺口：这两个入口原先直写 HKCU\Environment（自带校验集与
// trusted 入口不同，但落盘是同一家注册表）。区分性：撤销 user 级路由，本守卫
// 立即红（真实 HKCU 出现探针 / 内存后端读不到）。
func TestGenericEnvVarAPIs_RouteUserLevelThroughSeam(t *testing.T) {
	restore := UseInMemoryUserEnvForTesting(nil)
	defer restore()

	if _, ok := enumerateUserEnvVarsFromRegistry()["XMDEV_SEAM_PROBE"]; ok {
		t.Fatal("前置条件失败：真实 HKCU 已存在 XMDEV_SEAM_PROBE（疑似历史红探针残留）")
	}

	if err := SetEnvVar("XMDEV_SEAM_PROBE", "probe", "user"); err != nil {
		t.Fatalf("SetEnvVar(user): %v", err)
	}
	if got, err := GetUserEnvVarTrusted("XMDEV_SEAM_PROBE"); err != nil || got != "probe" {
		t.Fatalf("SetEnvVar(user) 应经接缝写入内存后端: got %q, err %v", got, err)
	}
	if v, ok := enumerateUserEnvVarsFromRegistry()["XMDEV_SEAM_PROBE"]; ok {
		t.Fatalf("接缝被绕过：SetEnvVar(user) 直写了真实 HKCU（%s=%q）", "XMDEV_SEAM_PROBE", v)
	}

	if err := DeleteEnvVar("XMDEV_SEAM_PROBE", "user"); err != nil {
		t.Fatalf("DeleteEnvVar(user): %v", err)
	}
	if _, err := GetUserEnvVarTrusted("XMDEV_SEAM_PROBE"); err == nil {
		t.Fatal("DeleteEnvVar(user) 应经接缝删除内存后端中的值")
	}
}

// ---- 守卫 5：GetUserEnvVarsBatch 必须返回调用方显式指定的非白名单名 ----
//
// 第四轮复核发现的缺陷：批量读取内部做白名单过滤，把调用方（hostsnapshot.Capture
// 的自定义 EnvKeys，如守卫自检的命名空间探针名）明确要追踪的变量**静默丢弃**——
// 与「按名单读取」的契约矛盾（非 Windows 桩从来不过滤）。区分性：恢复过滤，
// 本守卫立即红（探针名缺失）。
func TestGetUserEnvVarsBatch_ReturnsExplicitlyRequestedNonWhitelistedNames(t *testing.T) {
	const probeName = "XMDEV_BATCH_PROBE" // 刻意非白名单
	restore := UseInMemoryUserEnvForTesting(map[string]string{
		probeName:   "probe-value",
		"JAVA_HOME": `C:\Java`,
	})
	defer restore()

	if util.IsAllowedEnvVarName(probeName) {
		t.Fatalf("前置条件失败：%s 应当不在白名单内（否则本守卫失去区分性）", probeName)
	}

	got, err := GetUserEnvVarsBatch([]string{probeName, "JAVA_HOME", "MISSING_VAR"})
	if err != nil {
		t.Fatalf("GetUserEnvVarsBatch: %v", err)
	}
	if v, ok := got[probeName]; !ok || v != "probe-value" {
		t.Fatalf("显式请求的非白名单名被丢弃（白名单过滤回归？）: got %q, ok=%v", v, ok)
	}
	if v, ok := got["JAVA_HOME"]; !ok || v != `C:\Java` {
		t.Fatalf("白名单名应照常返回: got %q, ok=%v", v, ok)
	}
	if v, ok := got["MISSING_VAR"]; !ok || v != "" {
		t.Fatalf("不存在的名应返回空值（与单值读取的错误语义对齐的是 Capture 的 map 语义）: got %q, ok=%v", v, ok)
	}
}
