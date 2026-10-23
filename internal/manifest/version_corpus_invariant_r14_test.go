package manifest

import (
	"path/filepath"
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// 第十四轮审计（全盘考量）：随包 manifest 里的每个版本名必须**同时**满足两个
// 判据，否则会出现「安装装不上」或「卸载不重指向」这类跨模块回归。
//
// # 为什么需要这条守卫（它防的是「改一处、坏另一处」）
//
// 20261021 审计 P1-5（阶段 2）把 `Plugin.InstallDir` 从 fail-open 改成
// **fail-closed**：校验失败返回 `ErrUnsafeInstallPath` 并中止，不再替换成
// `unsafe-version-blocked` 继续。校验用的是 `util.SafeName(version)`。
//
// 同时 P1-4 新增 `util.LooksLikeVersionDirName`，并让 `ListVersionDirNames`
// 用它过滤目录；而 `installer.findOtherVersion`（卸载时决定重指向到哪个版本）
// 正是基于 `ListVersionDirNames` 的结果。
//
// 于是**同一个版本名现在被两套判据同时约束**：
//
//	SafeName 不通过        → 安装直接失败（fail-closed），工具装不上；
//	LooksLikeVersionDirName 不通过 → 该版本不进枚举，卸载时不会被选为重指向目标
//	                              → 明明还有别的版本，工具却被停用。
//
// 两者都只在**运行时**才暴露，且都只在某个具体工具/版本上暴露 —— 单点测试
// （手挑几个版本名）覆盖不到。唯一可靠的做法是：**拿随包的真实语料全量跑一遍**。
//
// 本守卫因此遍历 `manifests/*.yaml` 里**所有**工具与**所有**版本、以及
// `latest`/`lts` 别名，逐个断言。任何人日后收紧 `SafeName` 或
// `LooksLikeVersionDirName`（例如再「顺手」拒绝某个字符），只要它踩到随包版本，
// 这里立刻红 —— 而不是等到用户装那个工具时才炸。
//
// 本机实测基线（20261023）：53 个 manifest、166 个版本 token，全部通过。
func TestShippedVersionsSatisfyInstallAndEnumerationPredicates(t *testing.T) {
	dir := filepath.Join("..", "..", "manifests")
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog(%s): %v —— 守卫无法在空语料上静默通过", dir, err)
	}

	// **防空转**：语料读歪了（路径变更/解析失败但返回空目录）会让下面的循环
	// 一个都不跑，测试假绿。用数量下界把这种情况变成红灯。
	//
	// 下界取 40：随包基线是 53，留出「合理增删」余量，但绝不允许「几乎为空」。
	const minTools = 40
	if n := len(cat.Tools()); n < minTools {
		t.Fatalf("只加载到 %d 个工具（< %d）—— 语料路径或解析可疑，本守卫会在空集上假绿",
			n, minTools)
	}

	var checkedTools, checkedVersions int
	for _, tool := range cat.Tools() {
		checkedTools++

		// 1. 工具名：安装路径 `<install>/<tool>/<version>` 的第一个分量。
		if _, serr := util.SafeName(tool.Name); serr != nil {
			t.Errorf("工具 %q 的名字通不过 util.SafeName：%v —— "+
				"InstallDir 已 fail-closed，该工具将**完全无法安装**", tool.Name, serr)
		}

		// 2. 每个版本：既要能装（SafeName），也要能被枚举/重指向（LooksLike…）。
		for _, v := range tool.VersionList {
			checkedVersions++
			assertVersionPredicates(t, tool.Name, v.Version)
		}

		// 3. latest / lts 是**别名**，最终会被解析成具体版本；但 manifest 里若把它
		//    写成一个不含数字的字面量（如 "stable"），同样会被这两套判据拒绝。
		//    这里如实检查：它们要么为空，要么本身也是合法版本名。
		if tool.Latest != "" {
			assertVersionPredicates(t, tool.Name+" (latest)", tool.Latest)
		}
		if tool.LTS != "" {
			assertVersionPredicates(t, tool.Name+" (lts)", tool.LTS)
		}
	}

	// 记录实际覆盖量，便于有人收紧判据时判断红/绿是否可信。
	t.Logf("已校验 %d 个工具、%d 个版本名（含 latest/lts 别名）", checkedTools, checkedVersions)

	// 下界同样防「解析出工具但版本全丢」。
	if checkedVersions < 100 {
		t.Fatalf("只校验到 %d 个版本名（< 100）—— 版本解析可疑", checkedVersions)
	}
}

// assertVersionPredicates 对单个版本名断言两个判据，并给出**可操作**的失败原因。
func assertVersionPredicates(t *testing.T, tool, version string) {
	t.Helper()
	if version == "" {
		// 空版本本身是 manifest 缺陷；InstallDir 会以 ErrUnsafeInstallPath 拒绝。
		t.Errorf("%s: 存在空版本名 —— InstallDir 会 fail-closed 拒绝安装", tool)
		return
	}
	if _, err := util.SafeName(version); err != nil {
		t.Errorf("%s: 版本 %q 通不过 util.SafeName：%v\n"+
			"  后果：Plugin.InstallDir 已 fail-closed（P1-5 阶段 2），该版本**装不上**。\n"+
			"  若这是判据过严：请放宽 SafeName，而不是删掉本守卫。", tool, version, err)
	}
	if !util.LooksLikeVersionDirName(version) {
		t.Errorf("%s: 版本 %q 通不过 util.LooksLikeVersionDirName\n"+
			"  后果：它不会出现在 ListVersionDirNames 里，因此\n"+
			"        installer.findOtherVersion 永远不会选它作为卸载后的重指向目标\n"+
			"        —— 明明还有别的版本，工具却被停用。\n"+
			"  若这是判据过严：请放宽判据，而不是删掉本守卫。", tool, version)
	}
}

// TestShippedVersionNamesDoNotCollideOnCaseInsensitiveFS 是一个**跨平台**不变量：
// 同一工具下不得出现「仅大小写不同」的两个版本名。
//
// Windows/macOS 的默认文件系统大小写不敏感，`install/<tool>/1.0.0` 与
// `install/<tool>/1.0.0-ALPHA`… 这类碰撞会让两个版本指向同一个目录：
// 后装的覆盖先装的、卸载一个等于卸载两个。这类缺陷在 Linux CI 上**永远测不出来**，
// 只能靠静态语料检查提前拦住。
func TestShippedVersionNamesDoNotCollideOnCaseInsensitiveFS(t *testing.T) {
	dir := filepath.Join("..", "..", "manifests")
	cat, err := LoadCatalog(dir)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(cat.Tools()) < 40 {
		t.Fatalf("只加载到 %d 个工具 —— 语料可疑，本守卫会假绿", len(cat.Tools()))
	}

	for _, tool := range cat.Tools() {
		seen := make(map[string]string, len(tool.VersionList))
		for _, v := range tool.VersionList {
			key := lowerASCII(v.Version)
			if prev, ok := seen[key]; ok {
				t.Errorf("%s: 版本 %q 与 %q 在大小写不敏感文件系统上是**同一个目录** ——"+
					"后装的会覆盖先装的，卸载一个等于卸载两个", tool.Name, prev, v.Version)
				continue
			}
			seen[key] = v.Version
		}
	}
}

// lowerASCII 只折叠 ASCII 大小写。版本名是 ASCII 标识，用 Unicode 折叠会引入
// 与文件系统行为不一致的判据（NTFS 用的是自己的上表规则）。
func lowerASCII(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
