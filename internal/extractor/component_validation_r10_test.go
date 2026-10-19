package extractor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 第十轮审计 P3-6（P3）守卫：isSafePath 只校验**最终分量**
// （`filepath.Base(absOut)`），于是：
//
//   - `bin./evil.txt` + `bin/legit.txt` → 两个本应不同的目录在 Windows 上
//     被「去尾点」折叠成同一个 `bin`，evil.txt 静默落进 bin/；
//   - `sub/NUL/x.txt` → 写到 `sub` 后抛出晦涩的 OS 错误
//     （"The system cannot find the path specified"），完全无法定位到条目。
//
// 修复：**逐分量**校验尾随点/空格与 Windows 保留设备名，并对非法分量给出
// 含「条目名 + 非法分量」的可操作错误。
//
// 平台语义：尾随点/空格折叠与保留设备名都是 **Windows 文件系统语义**——
// 在 Unix 上 `bin.` 与 `NUL` 是合法且互不冲突的目录名，因此该校验与
// isSafePath 中既有的同名检查一样按 runtime.GOOS 收窄，避免拒绝合法的
// Unix 归档（保持与既有代码结构一致）。非 Windows 上用例断言
// 「合法名不被误拒」这一跨平台不变量。

// requireWindowsFoldSemantics 在非 Windows 上跳过「折叠」类断言。
func requireWindowsFoldSemantics(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("尾随点/空格折叠与保留设备名是 Windows 文件系统语义；Unix 上这些名字合法且不冲突")
	}
}

// TestBadComponent_LegalNamesAccepted 跨平台不变量：合法分量在任何平台都
// 不得被判为非法（否则会把正常归档拒之门外，并连带 RemoveAll(dst)）。
//
// **本机实测的 Windows 真实语义**（Go os.WriteFile + os.ReadDir 目录枚举）：
//
//	分量          写/读回        真实目录项
//	CON           "X"           true     ← 普通文件
//	con           "X"           **false** ← 落进控制台设备，目录里没有
//	NUL           ""            false    ← 落进空设备，数据丢失
//	con.bak       "X"           true     ← 普通文件
//	con.txt.bak   "X"           true     ← 普通文件
//	console       "X"           true     ← 普通文件
//	nul.bak       "X"           true     ← 普通文件
//
// 结论：**只有完整分量恰好等于保留名时才有害**，扩展名完全不参与判定
// （`con.txt` 与 `con` 的差别不在扩展名，而在 `con.txt` 是普通文件、
// `con` 是设备名）。因此「剥掉第一个 `.` 之前的部分再比对」是**误报源**。
func TestBadComponent_LegalNamesAccepted(t *testing.T) {
	for _, rel := range []string{
		"bin/legit.txt",
		"..foo.txt",
		"..cache/x.txt",
		"...x",
		"bin/tool.exe",
		"a.b/c.d",
		"console/real.txt", // 保留名必须是**完整**分量；"console" 只是以 CON 开头
		"real.txt/nullable.txt",
		"con.txt",              // 本机实测：普通文件（真实目录项）
		"nul.txt",              // 本机实测：普通文件（真实目录项）
		"con.bak",              // 本机实测：普通文件
		"con.txt.bak/real.txt", // 本机实测：普通文件
		"aux.x.y",              // 本机实测：普通文件
	} {
		if bad := badComponent(rel); bad != "" {
			t.Errorf("badComponent(%q) = %q，合法分量被误判为非法（会拒绝正常归档，并连带删掉目标目录）", rel, bad)
		}
	}
}

// TestBadComponent_RejectsWindowsFolds 是缺陷的最小复现（纯谓词层）：
// 逐分量检查必须命中尾随点/空格与保留设备名，且**返回具体分量**。
func TestBadComponent_RejectsWindowsFolds(t *testing.T) {
	requireWindowsFoldSemantics(t)

	cases := map[string]string{
		"bin./evil.txt":  "bin.", // 尾随点（中间分量）—— 实测与 "bin" 折叠
		"evil./x.txt":    "evil.",
		"sub/NUL/x.txt":  "NUL", // 保留设备名（中间分量）—— 实测目录被静默丢弃
		"sub/aux/y.txt":  "aux", // 大小写不敏感
		"sub/com1/y.txt": "com1",
		"a/b /c.txt":     "b ",       // 尾随空格 —— 实测与 "b" 折叠
		"a/lpt9.txt":     "lpt9.txt", // 仅"lpt9.txt"不是保留名（见 LegalNames），
		// 这里断言的是**尾随点**？不 —— lpt9.txt 无尾随点。它应是合法名。
		".../x.txt": "...", // 全点分量（Windows 上折叠为空名）
	}
	// lpt9.txt 本机实测是**合法**普通文件（保留名要求整体相等），
	// 因此从"应被拒绝"的集合里移除，避免把误报当成契约。
	delete(cases, "a/lpt9.txt")
	for rel, want := range cases {
		got := badComponent(rel)
		if got == "" {
			t.Errorf("badComponent(%q) = \"\"：非法分量未被识别（P3-6）", rel)
			continue
		}
		if got != want {
			t.Errorf("badComponent(%q) = %q, want %q（错误信息必须指向具体分量）", rel, got, want)
		}
	}
	// 显式钉住：lpt9.txt 是合法名（防止有人"顺手"把扩展名规则加回来）。
	if bad := badComponent("a/lpt9.txt"); bad != "" {
		t.Errorf("badComponent(\"a/lpt9.txt\") = %q，但本机实测 lpt9.txt 是合法普通文件", bad)
	}
}

// TestIsSafePath_RejectsTrailingDotComponent 断言安全门本身也逐分量生效
// （isSafePath 是 zip/tar/symlink/7z 四条路径共享的判据）。
//
// 注意：`filepath.Abs`/`Clean` **自己**就会在 Windows 上剥离尾随点
// （实测 `Abs(C:\...\001\bin.\evil.txt)` = `C:\...\001\bin\evil.txt`），
// 所以拿 `filepath.Join(dst, "bin.", ...)` 构造的输入在被检查前就已经
// 被折叠掉了 —— 那条断言测的不是本函数。这里用 Clean 不会改写的形态：
// 保留设备名（中间分量）与尾随**空格**分量。
func TestIsSafePath_RejectsTrailingDotComponent(t *testing.T) {
	requireWindowsFoldSemantics(t)
	dst := t.TempDir()
	// 保留设备名（中间分量）：filepath.Abs 不改写它，必须由逐分量检查拦住。
	if isSafePath(dst, filepath.Join(dst, "sub", "NUL", "x.txt")) {
		t.Error("isSafePath 放行了含保留设备名分量的路径（P3-6）")
	}
	// 尾随空格分量：同样由逐分量检查拦住。
	if isSafePath(dst, filepath.Join(dst, "b ", "c.txt")) {
		t.Error("isSafePath 放行了含尾随空格分量的路径 —— 两个目录会被折叠成一个（P3-6）")
	}
	// 对照：最终分量规则（既有行为）继续生效。
	if isSafePath(dst, filepath.Join(dst, "sub", "COM1")) {
		t.Error("isSafePath 放行了以保留设备名结尾的路径（既有最终分量规则被回归）")
	}
}

// TestExtractZip_TrailingDotComponentRejectedActionably 是端到端复现：
// 修复前 err=nil，且 evil.txt 静默落进被折叠出来的 bin/。
func TestExtractZip_TrailingDotComponentRejectedActionably(t *testing.T) {
	requireWindowsFoldSemantics(t)

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "fold.zip")
	dstDir := filepath.Join(tmpDir, "out")

	// 顺序刻意让合法条目先落盘，再触发非法条目 —— 缺陷正是"写到一半才发现"。
	createTestZip(t, zipPath, []string{
		"bin/legit.txt|legit",
		"bin./evil.txt|evil",
	})

	err := Extract(zipPath, dstDir, "zip", 0)
	if err == nil {
		// 缺陷现场：err=nil 且 evil 落进折叠后的 bin/。
		merged := filepath.Join(dstDir, "bin", "evil.txt")
		if _, serr := os.Stat(merged); serr == nil {
			t.Fatalf("P3-6 复现：bin./evil.txt 被静默折叠进 %s（两个本应不同的目录合并了）", merged)
		}
		t.Fatalf("含非法分量（bin.）的条目未被拒绝")
	}
	// 错误必须可操作：同时含条目名与非法分量。
	if !strings.Contains(err.Error(), "bin./evil.txt") {
		t.Errorf("错误信息未含条目名：%v", err)
	}
	if !strings.Contains(err.Error(), "bin.") {
		t.Errorf("错误信息未含非法分量 bin.：%v", err)
	}
	// 非法条目绝不能落进折叠出来的目录。
	if _, serr := os.Stat(filepath.Join(dstDir, "bin", "evil.txt")); serr == nil {
		t.Errorf("非法条目仍然落进了 %s", filepath.Join(dstDir, "bin", "evil.txt"))
	}
}

// TestExtractZip_ReservedNameComponentRejectedActionably 覆盖审计实测的
// 第二个形态：`sub/NUL/x.txt` 修复前抛出晦涩的 OS 错误。
func TestExtractZip_ReservedNameComponentRejectedActionably(t *testing.T) {
	requireWindowsFoldSemantics(t)

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "reserved.zip")
	dstDir := filepath.Join(tmpDir, "out")

	createTestZip(t, zipPath, []string{
		"sub/ok.txt|ok",
		"sub/NUL/x.txt|bad",
	})

	err := Extract(zipPath, dstDir, "zip", 0)
	if err == nil {
		t.Fatalf("含 Windows 保留设备名分量的条目未被拒绝（P3-6）")
	}
	if !strings.Contains(err.Error(), "sub/NUL/x.txt") {
		t.Errorf("错误信息未含条目名，无法定位问题条目：%v", err)
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Errorf("错误信息未含非法分量 NUL：%v", err)
	}
	// 不得再是那条晦涩的 OS 错误。
	if strings.Contains(err.Error(), "The system cannot find the path specified") {
		t.Errorf("仍然是晦涩的 OS 错误，未给出可操作信息：%v", err)
	}
}

// TestExtractTarGz_InvalidComponentRejectedActionably 覆盖 tar 侧同一判据。
func TestExtractTarGz_InvalidComponentRejectedActionably(t *testing.T) {
	requireWindowsFoldSemantics(t)

	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "fold.tar.gz")
	dstDir := filepath.Join(tmpDir, "out")

	createTestTarGz(t, tarPath, []string{
		"bin./evil.txt|evil",
	})

	err := Extract(tarPath, dstDir, "tar.gz", 0)
	if err == nil {
		t.Fatalf("tar 侧含非法分量（bin.）的条目未被拒绝（P3-6）")
	}
	if !strings.Contains(err.Error(), "bin./evil.txt") || !strings.Contains(err.Error(), "bin.") {
		t.Errorf("错误信息不可操作（应含条目名与非法分量）：%v", err)
	}
}
