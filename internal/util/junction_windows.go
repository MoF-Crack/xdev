//go:build windows

package util

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createJunction 在 Windows 上创建目录 junction（重解析点）。
//
// 第十一轮审计（Round 10 报告待办 4）：**改为原生 FSCTL_SET_REPARSE_POINT**，
// 不再经 cmd.exe / mklink。
//
// 历史脉络：
//   - 旧实现 `exec.Command("cmd","/c","mklink","/J",link,target)` 有**命令注入**
//     （Round 9 P1-1，`&` 是合法文件名字符，cmd.exe 会二次解析）。Round 9 的修法
//     是自己构造 `SysProcAttr.CmdLine` 并对每个路径加双引号、再拒绝无法安全表达的
//     字符（`"` `%` `!` 控制字符）。
//   - 那个修法**安全但有限制**：含 `%` / `!` / `"` 的路径被直接拒绝（例如
//     `D:\100%Portable` 就无法使用），且每次激活都要**起一个 cmd.exe 子进程**。
//   - 仓库注释记载「手写 REPARSE_DATA_BUFFER 曾在不同路径长度下脆弱、被回退过」
//     （Spec 032），因此 Round 9/10 都把它列为「应作为独立改动 + 配足路径长度
//     回归矩阵」。本轮补齐了这个矩阵（见 junction_native_r11_test.go）。
//
// 实现要点（**这是 Spec 032 回退的那个坑**）：
//
//	mountPointReparseBuffer 里的 4 个 Length/Offset 字段**全部是字节**偏移，
//	且 PathBuffer 是 UTF-16，因此 `len(path)*2`（字符数 → 字节数）必须算对；
//	` SubstituteNameOffset` 恒为 0。整个 REPARSE_DATA_BUFFER 的 `ReparseDataLength`
//	只算「buffer 头 + 路径字节」，**不含** 8 字节的 tag+len 头。
//	历史实现出问题正是因为把字符数当字节数（短路径侥幸正确 —— ASCII 短名时
//	字符数≈字节数的一半偏移恰好落在可容忍区间；长路径立刻错位）。
//
// 目标路径必须写成 NT 命名空间形式 `\??\C:\...`（mklink 内部也是这么做的）。
// 相对路径先转绝对。
//
// 失败时**不**回退到 mklink：原生调用失败说明真的有问题（权限/路径过长/
// 目标不是目录），静默换个机制会让问题更难诊断。错误信息里带上两个路径。
func createJunction(link, target string) error {
	if link == "" || target == "" {
		return fmt.Errorf("refusing to create a junction: empty path (link=%q target=%q)", link, target)
	}
	absTarget, err := filepathAbsLong(target)
	if err != nil {
		return fmt.Errorf("create junction %s -> %s: resolve target: %w", link, target, err)
	}
	absLink, err := filepathAbsLong(link)
	if err != nil {
		return fmt.Errorf("create junction %s -> %s: resolve link: %w", link, target, err)
	}

	// 目标必须以 NT 命名空间形式给出。
	ntTarget := absTarget
	if !strings.HasPrefix(ntTarget, `\??\`) {
		ntTarget = `\??\` + ntTarget
	}

	// 先建出**空的**链接目录：junction 只能在已存在的空目录上设置重解析点。
	if err := os.Mkdir(absLink, 0o755); err != nil {
		return fmt.Errorf("create junction %s -> %s: mkdir link: %w", link, target, err)
	}

	buf, err := buildMountPointReparseBuffer(ntTarget, ntTarget)
	if err != nil {
		_ = os.Remove(absLink)
		return fmt.Errorf("create junction %s -> %s: %w", link, target, err)
	}

	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr(absLink),
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		_ = os.Remove(absLink)
		return fmt.Errorf("create junction %s -> %s: open link: %w", link, target, err)
	}
	defer windows.CloseHandle(h)

	var returned uint32
	if err := windows.DeviceIoControl(
		h,
		windows.FSCTL_SET_REPARSE_POINT,
		&buf[0],
		uint32(len(buf)),
		nil,
		0,
		&returned,
		nil,
	); err != nil {
		_ = os.Remove(absLink)
		return fmt.Errorf("create junction %s -> %s: set reparse point: %w", link, target, err)
	}
	return nil
}

// filepathAbsLong 返回绝对路径，且**保持**长路径前缀（若已存在）。
//
// 刻意不用 filepath.Abs：它走 GetFullPathName，会**规范化** `\\?\` 前缀，
// 而 junction 的 SubstituteName 需要的是我们可以自行控制的形式。
func filepathAbsLong(p string) (string, error) {
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\??\`) {
		return p, nil
	}
	abs, err := syscall.FullPath(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// buildMountPointReparseBuffer 构造 IO_REPARSE_TAG_MOUNT_POINT 的
// REPARSE_DATA_BUFFER。
//
// 布局（全部小端）：
//
//	 0  uint32 ReparseTag           = 0xA0000003        (实测 03 00 00 a0)
//	 4  uint16 ReparseDataLength   = 8 + pathBytes     (实测 ec 00 = 236)
//	 6  uint16 Reserved            = 0
//	 8  uint16 SubstituteNameOffset = 0                (实测 00 00)
//	10  uint16 SubstituteNameLength = len(subst)*2     (实测 74 00 = 116) ← **字节数**
//	12  uint16 PrintNameOffset      = len(subst)*2 + 2 (实测 76 00 = 118) ← **+2**
//	14  uint16 PrintNameLength      = len(print)*2     (实测 6c 00 = 108) ← **字节数**
//	16  wchar  PathBuffer[...]      = subst + NUL + print + NUL
//
// 上面括号里的值是**本机用真实 mklink 产物 dump 出来的**（`FSCTL_GET_REPARSE_POINT`
// 逐字节打印），不是照文档抄的。
//
// 三个必须踩准的点（Spec 032 回退就是因为这些）：
//
//  1. **Length 字段是字节数**（UTF-16 ⇒ 字符数×2），不是字符数。纯 ASCII 短名时
//     近似巧合地「看起来能用」，长路径/非 ASCII 立刻 `ERROR_INVALID_REPARSE_DATA`。
//  2. **PrintNameOffset 必须跳过 Substitute 的 NUL 终止符**（substBytes + 2）。
//     写成 substBytes 会让它指向那个 NUL 本身 —— 内核判定数据非法。
//     **本文件初版就是这个错误**，实测报
//     `set reparse point: The data present in the reparse point buffer is invalid`，
//     矩阵里**全部**用例（含最短路径）一起红。这正是「必须有路径长度矩阵 + 与真实
//     mklink 产物逐字节对照」的价值所在。
//  3. **ReparseDataLength 不含 tag(4)+len(2)+reserved(2) 这 8 字节头**，
//     只算「4 个 uint16 + PathBuffer」= 8 + pathBytes。
func buildMountPointReparseBuffer(subst, print string) ([]byte, error) {
	substU := windows.StringToUTF16(subst) // 含结尾 NUL
	printU := windows.StringToUTF16(print)

	substLen := (len(substU) - 1) * 2 // 字节数，不含 NUL
	printLen := (len(printU) - 1) * 2

	// PrintName 紧接在 Substitute 的 **NUL 之后**。
	printOffset := substLen + 2

	// PathBuffer = (subst + NUL) + (print + NUL)
	pathBytes := len(substU)*2 + len(printU)*2
	// reparseDataLength = 四个 uint16 + PathBuffer = 8 + pathBytes
	dataLen := 8 + pathBytes
	total := 8 + dataLen // + tag(4) + len(2) + reserved(2)

	if dataLen > 0xFFFF || printOffset > 0xFFFF {
		return nil, fmt.Errorf("junction path too long: reparse data length %d (print offset %d) exceeds 65535 bytes "+
			"(substitute=%q)", dataLen, printOffset, subst)
	}

	buf := make([]byte, total)
	le := func(off int, v uint32, size int) {
		for i := 0; i < size; i++ {
			buf[off+i] = byte(v >> (8 * i))
		}
	}
	le(0, windows.IO_REPARSE_TAG_MOUNT_POINT, 4)
	le(4, uint32(dataLen), 2)
	le(6, 0, 2)                    // Reserved
	le(8, 0, 2)                    // SubstituteNameOffset
	le(10, uint32(substLen), 2)    // SubstituteNameLength（字节）
	le(12, uint32(printOffset), 2) // PrintNameOffset（字节，**跳过 NUL**）
	le(14, uint32(printLen), 2)    // PrintNameLength（字节）

	off := 16
	for _, u := range substU {
		buf[off] = byte(u)
		buf[off+1] = byte(u >> 8)
		off += 2
	}
	for _, u := range printU {
		buf[off] = byte(u)
		buf[off+1] = byte(u >> 8)
		off += 2
	}
	_ = unsafe.Sizeof(buf)
	return buf, nil
}
