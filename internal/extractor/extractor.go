// Package extractor expands structural archive records into private staging.
package extractor

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/xmdemt/xdev/internal/util"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Extract uses the standard archive limits. See ExtractWithOptions for larger trusted archives.
func Extract(src, dst, format string, strip int) error {
	limits, err := ConfiguredBudget()
	if err != nil {
		return err
	}
	return ExtractWithOptions(context.Background(), src, dst, format, strip, limits)
}
func (x *extraction) unpack(src, format string, strip int) error {
	switch strings.ToLower(format) {
	case "zip":
		return x.zip(src, strip)
	case "7z", "exe", "pkg":
		return x.container(src, format, strip)
	case "dmg":
		return x.extractDmg(src)
	case "appimage":
		out, err := x.entry(filepath.Base(src), 0)
		if err != nil {
			return err
		}
		if err = x.copyRegular(src, out, 0755); err != nil {
			return err
		}
		return os.Chmod(out, 0755)
	case "tar.xz", "txz", "tar.zst", "tzst":
		var r readCloser
		var err error
		if strings.HasSuffix(strings.ToLower(format), "xz") {
			r, err = newXZReader(src)
		} else {
			r, err = newZstdReader(src)
		}
		if err != nil {
			return err
		}
		defer r.Close()
		return x.tar(r, strip)
	case "tar", "tar.gz", "tgz", "tar.bz2", "tbz2":
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		var r io.Reader = f
		switch strings.ToLower(format) {
		case "tar.gz", "tgz":
			g, e := gzip.NewReader(f)
			if e != nil {
				return e
			}
			defer g.Close()
			r = g
		case "tar.bz2", "tbz2":
			r = bzip2.NewReader(f)
		}
		return x.tar(r, strip)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}
func (x *extraction) copyRegular(src, out string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	fi, e := in.Stat()
	if e != nil {
		return e
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", src)
	}
	return x.file(out, mode, fi.Size(), in)
}
func extractZip(src, dst string, strip int) error { return Extract(src, dst, "zip", max(strip, 0)) }
func (x *extraction) zip(src string, strip int) error {
	r, e := zip.OpenReader(src)
	if e != nil {
		return e
	}
	defer r.Close()
	for _, f := range r.File {
		out, e := x.entry(f.Name, strip)
		if e != nil {
			return fmt.Errorf("zip slip: %w", e)
		}
		if out == "" {
			continue
		}
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(out, 0755); e != nil {
				return e
			}
			continue
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("unsupported ZIP member type: %s", f.Name)
		}
		if e = x.checkSize(int64(f.UncompressedSize64)); e != nil {
			return e
		}
		in, e := f.Open()
		if e != nil {
			return e
		}
		e = errors.Join(x.file(out, f.Mode(), int64(f.UncompressedSize64), in), in.Close())
		if e != nil {
			return e
		}
	}
	return nil
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func (x *extraction) tar(r io.Reader, strip int) error {
	tr := tar.NewReader(cancelReader{x.ctx, r})
	for {
		h, e := tr.Next()
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		out, e := x.entry(h.Name, strip)
		if e != nil {
			return fmt.Errorf("tar slip: %w", e)
		}
		if e = x.checkSize(h.Size); e != nil {
			return e
		}
		if out == "" {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			e = os.MkdirAll(out, 0755)
		case tar.TypeReg, tar.TypeRegA:
			e = x.file(out, h.FileInfo().Mode(), h.Size, tr)
		case tar.TypeSymlink:
			e = x.symlink(x.dst, out, h.Linkname)
		case tar.TypeLink:
			// Count the member once; revalidate the full raw archive-relative target.
			x.entries--
			target, err := x.entry(h.Linkname, strip)
			if err != nil {
				return err
			}
			if target == "" {
				continue
			}
			fi, err := os.Lstat(target)
			if os.IsNotExist(err) {
				log.Printf("[extractor] skipping missing hard link target %q", h.Linkname)
				continue
			}
			if err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("invalid hard link target %q", h.Linkname)
			}
			if e = x.checkSize(fi.Size()); e != nil {
				return e
			}
			if e = os.MkdirAll(filepath.Dir(out), 0755); e != nil {
				return e
			}
			if e = os.Link(target, out); e != nil {
				e = x.copyRegular(target, out, fi.Mode())
			} else {
				x.total += fi.Size()
			}
		default:
			return fmt.Errorf("unsupported tar entry type %d: %q", h.Typeflag, h.Name)
		}
		if e != nil {
			return e
		}
	}
}

// hdiutilCmd is the hdiutil command used by extractDmg. It is a package
// variable so tests can override it with a mock script (same pattern as
// the zstd/xz external-command fallbacks).
var hdiutilCmd = "hdiutil"

// detachDMG unmounts the image at mountDir: first a normal detach, then a
// forced detach (-force) if the first attempt fails. Returns a joined error
// when both attempts fail. 若 hdiutil 本身不可执行（exec.Error，如命令不存在），
// -force 同样无意义，直接返回原错误。
func detachDMG(mountDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first := exec.CommandContext(ctx, hdiutilCmd, "detach", mountDir)
	util.HideWindow(first)
	if err := first.Run(); err == nil {
		return nil
	} else if isExecNotFound(err) {
		return fmt.Errorf("hdiutil detach %s: %w", mountDir, err)
	} else {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel2()
		force := exec.CommandContext(ctx2, hdiutilCmd, "detach", "-force", mountDir)
		util.HideWindow(force)
		err2 := force.Run()
		if err2 == nil {
			return nil
		}
		return fmt.Errorf("hdiutil detach %s failed (normal: %v; force: %v)", mountDir, err, err2)
	}
}

// isExecNotFound reports whether err indicates the command itself could not
// be executed (exec.Error: not found / not executable), as opposed to the
// command running and returning a non-zero exit.
func isExecNotFound(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr)
}

// extractDmg extracts a macOS .dmg disk image: attach it read-only with
// hdiutil, copy every .app bundle into dst, then detach.
//
// dmg 解包仅支持 macOS（依赖 hdiutil）。非 macOS 平台直接返回明确错误。
//
// 错误处理（023-A3-02）：
//   - attach 失败：尽力 detach 已知挂载点（可能部分挂载）后返回 attach 错误；
//   - detach 失败：先普通 detach，再尝试 detach -force，仍失败则并入返回值，
//     避免"解包成功但镜像残留挂载"。
func (x *extraction) extractDmg(src string) (retErr error) {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("dmg extraction is only supported on macOS (current: %s)", runtime.GOOS)
	}

	// 在系统临时目录创建挂载点（os.MkdirTemp 返回绝对路径，
	// hdiutil -mountpoint 要求绝对路径）。
	mountDir, err := os.MkdirTemp("", "xdev-dmg-*")
	if err != nil {
		return fmt.Errorf("create dmg mount dir: %w", err)
	}
	// 清理：无论成败都移除挂载点目录。
	defer os.Remove(mountDir)

	// hdiutil attach -nobrowse -readonly -mountpoint <mountDir> <src>
	attach := exec.CommandContext(x.ctx, hdiutilCmd, "attach", "-nobrowse", "-readonly", "-mountpoint", mountDir, src)
	util.HideWindow(attach)
	attach.Stdout = os.Stderr
	attach.Stderr = os.Stderr
	if err := attach.Run(); err != nil {
		// attach 失败：尽力卸载已知挂载点（可能部分挂载），仅记录不掩盖主错误。
		if dErr := detachDMG(mountDir); dErr != nil {
			log.Printf("[extractor] WARNING: cleanup detach after attach failure: %v", dErr)
		}
		return fmt.Errorf("hdiutil attach %s: %w", src, err)
	}
	// detach 在 defer 中执行，保证复制失败时镜像也被卸载；失败并入返回值。
	defer func() {
		if dErr := detachDMG(mountDir); dErr != nil {
			if retErr != nil {
				retErr = fmt.Errorf("%w; %v", retErr, dErr)
			} else {
				retErr = dErr
			}
		}
	}()

	// 复制挂载点下所有 .app 包到 dst（dmg 通常含一个 .app）。
	copied, err := x.copyAppsFromMount(mountDir)
	if err != nil {
		return err
	}
	if copied == 0 {
		return fmt.Errorf("no .app bundle found in dmg %s", src)
	}
	return nil
}

// ensureExecutableBits restores the POSIX execute bit for archives that cannot
// carry it. Go's archive/zip reports 0666 for entries created on FAT/Windows,
// so extracted binaries land non-executable and every shim exec fails with
// EACCES — invisible on Windows, where the permission bits are ignored.
//
// Heuristic: files under a "bin" directory, files without an extension, and
// common executable extensions get 0755. Anything else is left untouched.
func ensureExecutableBits(ctx context.Context, root string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Mode().Perm()&0o111 != 0 {
			return nil // already executable
		}
		base := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(base))
		inBin := strings.EqualFold(filepath.Base(filepath.Dir(path)), "bin")
		switch ext {
		case "", ".sh", ".bash", ".run", ".bin", ".appimage":
			// plausible executable
		default:
			if !inBin {
				return nil
			}
		}
		return os.Chmod(path, 0o755)
	})
}

// checkNoSymlinkAncestor verifies that no component of the output path
// between dst and out is a symlink. This prevents cross-entry symlink
// traversal attacks (H-1) where a previous tar entry creates a symlink
// that causes a later regular-file entry to write outside dst.
//
// For example, if entry 1 creates a symlink "subdir -> /tmp", then
// entry 2 writing "subdir/payload" would land in /tmp/payload even
// though the lexical path "subdir/payload" appears safe.
func checkNoSymlinkAncestor(dst, out string) error {
	absDst, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(filepath.Clean(out))
	if err != nil {
		return err
	}

	// Walk from dst downward, checking each component for symlinks.
	// We stop when we reach the output path.
	rel, err := filepath.Rel(absDst, absOut)
	if err != nil {
		return err
	}
	// If the output is the same as dst, nothing to check
	if rel == "." {
		return nil
	}

	current := absDst
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		fi, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent directory %q is a symlink — possible traversal attack", current)
		}
	}
	return nil
}

// windowsReservedDeviceNames lists the Windows reserved device names that
// cannot be used as a path component. Kept in one place because both the
// per-component validation (badComponent) and the destination check use it.
//
// **只匹配完整分量，不剥离扩展名** —— 本机实测（Windows 10，Go os.WriteFile +
// PowerShell [System.IO.File]）：
//
//	分量        写文件        读回        目录项
//	CON        OK            "X"        CON      ← 普通文件，可正常读写
//	NUL        OK            ""         （无）    ← 写进空设备，数据丢失
//	con.txt    OK            "X"        con.txt  ← 普通文件
//	nul.txt    OK            "X"        nul.txt  ← 普通文件
//	con.txt.bak OK           "X"        con.txt.bak
//	NUL（目录） OK            —          （无）    ← 目录被静默丢弃
//
// 即：**只有完整分量恰好等于保留名时才有害**；带扩展名的 `con.txt` / `nul.txt`
// 是**合法且真实存在**的文件（与 Win32 文档「CON.txt 是保留名」的说法在 Go 的
// 调用路径下不符 —— 以本机实测为准）。因此旧实现里「剥掉第一个 `.` 之前的部分
// 再比对」是**误报源**，会把 `con.txt.bak`、`real.txt/con.txt` 这类正常条目
// 判成非法并连带 `RemoveAll(dst)` 删掉整个目标目录。
var windowsReservedDeviceNames = []string{"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}

// isWindowsReservedDeviceName 报告 comp 是否是 Windows 保留设备名。
// 大小写不敏感，且**必须整体相等**（`con.txt` 不是保留名 —— 见上表）。
func isWindowsReservedDeviceName(comp string) bool {
	upper := strings.ToUpper(comp)
	for _, reserved := range windowsReservedDeviceNames {
		if upper == reserved {
			return true
		}
	}
	return false
}

// hasTrailingDotOrSpace 报告 comp 是否以点或空格结尾（Windows 会静默剥离，
// 使两个不同名字折叠成同一个 —— 实测 `bin.` 与 `bin` 折叠为一个目录项）。
//
// 注意：`..` / `.` 是相对引用而非"名字"，由调用方先行跳过。
func hasTrailingDotOrSpace(comp string) bool {
	return strings.TrimRight(comp, ". ") != comp
}

// badComponent validates EVERY path component (not just the final one) and
// returns the first component that Windows would mangle, or "" when the path
// is clean.
//
// 第十轮审计 P3-6（P3）：旧实现只校验 `filepath.Base(absOut)`（最终分量），
// 于是**中间**分量的 Windows 折叠语义完全没被检查：
//
//   - 条目 `bin./evil.txt` + `bin/legit.txt`：`bin.` 在 Windows 上去掉尾点
//     就是 `bin`，两个本应不同的目录被静默折叠成一个，evil.txt 落进 bin/；
//   - 条目 `sub/NUL/x.txt`：写到 `sub` 之后才抛出晦涩的 OS 错误
//     （"The system cannot find the path specified"），无法定位到条目。
//
// 这两条规则（尾随点/空格折叠、保留设备名）都是 **Windows 文件系统语义**，
// 因此与 isSafePath 中既有的同名检查一致，在非 Windows 上直接放行 ——
// Unix 上 `bin.` / `NUL` 是合法且互不冲突的名字，不该拒绝合法归档。
//
// 返回具体分量而不是 bool，是为了让调用方能给出**可操作**的错误
// （含条目名 + 非法分量）。
func badComponent(p string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	// 归档条目用 "/" 分隔，但 isSafePath 收到的是 filepath.Join 之后的本机
	// 路径（Windows 上是 "\"）；两种分隔符都拆。
	norm := strings.ReplaceAll(p, `\`, "/")
	for _, comp := range strings.Split(norm, "/") {
		// 空分量（绝对路径/重复分隔符）与相对引用（"." / ".."）不是"名字"，
		// 逃逸判定由 rel 层判据单独负责。
		if comp == "" || comp == "." || comp == ".." {
			continue
		}
		// 1) Windows 保留设备名：**整体相等**才算（`con.txt` 是合法真实文件，
		//    见 windowsReservedDeviceNames 的实测表）。
		if isWindowsReservedDeviceName(comp) {
			return comp
		}
		// 2) 尾随点/空格：Windows 静默剥离，使两个不同名字折叠成同一个
		//    （实测 `bin.` 与 `bin` 折叠为一个目录项）。全点/全空格分量
		//    （"..."）同样折叠为空名，一并拒绝。
		if hasTrailingDotOrSpace(comp) {
			return comp
		}
	}
	return ""
}

// isSafePath checks that the resolved output path stays within the
// destination directory.  This prevents Zip Slip / Tar Slip attacks
// where a malicious archive contains paths like "../../etc/passwd".
// On Windows, the comparison is case-insensitive to match the filesystem.
func isSafePath(dst, out string) bool {
	absDst, err := filepath.Abs(filepath.Clean(dst))
	if err != nil {
		return false
	}
	absOut, err := filepath.Abs(filepath.Clean(out))
	if err != nil {
		return false
	}
	// rel is needed both by the Windows component checks and by the
	// escape check below; compute it once.
	rel, relErr := filepath.Rel(absDst, absOut)
	// P3-6：分量校验必须用**原始** out 计算的 rel，不能用 absOut 的 ——
	// filepath.Abs 走 GetFullPathName，**已经**把 `bin.` 折叠成 `bin`
	// （本机实测：`Rel(abs)` = "bin\evil.txt"，证据在检查前就被销毁）。
	rawRel, rawErr := filepath.Rel(filepath.Clean(dst), filepath.Clean(out))
	// Security (M-8): Reject Windows special paths
	if runtime.GOOS == "windows" {
		// Reject UNC paths (\\?\, \\.\, \\server\share) — but only when the
		// DESTINATION is not itself UNC: with an UNC install root every
		// entry would otherwise be rejected as "zip slip" (ticket 38 /
		// P2-02). Archive-relative names starting with \\ are still blocked
		// by the rel-level check in the extractors.
		if strings.HasPrefix(absOut, `\\`) && !strings.HasPrefix(absDst, `\\`) {
			return false
		}
		// Reject cross-volume paths
		if filepath.VolumeName(absOut) != filepath.VolumeName(absDst) {
			return false
		}
		// Reject Alternate Data Streams (colon after drive letter)
		volLen := len(filepath.VolumeName(absOut))
		if volLen > 0 && strings.Contains(absOut[volLen:], ":") {
			return false
		}
		// Reject Windows reserved device names and trailing dots/spaces in
		// ANY component — not just the final one (P3-6). Checking only
		// filepath.Base let `bin./evil.txt` silently fold into `bin/` and
		// `sub/NUL/x.txt` fail later with an opaque OS error.
		//
		// 必须用 rawRel（见上）：absOut 里的 `bin.` 已被 filepath.Abs 折叠掉。
		if rawErr == nil && badComponent(rawRel) != "" {
			return false
		}
	}
	// Also check via filepath.Rel that the path does not escape.
	// This catches cases where Clean resolves .. but the relative
	// path still escapes (e.g. on case-insensitive filesystems).
	//
	// P3-5: the test must be "the path IS the parent" or "starts with
	// <parent><separator>". A bare strings.HasPrefix(rel, "..") also matched
	// LEGAL names that merely start with two dots (..foo.txt, ..cache/x.txt),
	// so a legitimate archive entry was rejected as zip-slip and the
	// extractors then removed the ENTIRE destination directory.
	if relErr == nil {
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	// Ensure the output path is under the destination directory.
	// We add a separator to prevent prefix matching issues
	// (e.g. /tmp/foo matching /tmp/foobar).
	// On Windows, paths are case-insensitive.
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absOut, absDst) ||
			strings.HasPrefix(strings.ToLower(absOut), strings.ToLower(absDst)+string(os.PathSeparator))
	}
	return strings.HasPrefix(absOut, absDst+string(os.PathSeparator)) || absOut == absDst
}

// getNlink returns the number of hard links for a file. On Windows it
// always returns 1 (hard link detection is unreliable). On Unix it
// reads the link count from the underlying syscall.Stat_t.
func getNlink(info os.FileInfo) uint64 {
	if runtime.GOOS == "windows" {
		return 1 // Windows doesn't reliably expose nlink
	}
	return getNlinkUnix(info)
}

// stripPath discards the first `strip` components of p.
//
// Returns (relativePath, true) when the path is non-empty after
// stripping, otherwise ("", false) meaning the entry should be
// skipped.
func stripPath(p string, strip int) (string, bool) {
	// F-20: clamp a negative strip. The value is manifest-controlled; the
	// pre-fix code only guarded the upper bound, so parts[-1:] panicked.
	if strip < 0 {
		strip = 0
	}
	p = filepath.Clean(p)
	if p == "." || p == "/" {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	if strip >= len(parts) {
		return "", false
	}
	rel := strings.Join(parts[strip:], "/")
	if rel == "" {
		return "", false
	}
	return rel, true
}

// gzipReaderAdapter bridges the *gzip.Reader return type to the
// io.ReadCloser signature our extractTar signature uses.
func gzipReaderAdapter(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

// bzip2ReaderAdapter wraps bzip2.NewReader to return io.ReadCloser.
func bzip2ReaderAdapter(r io.Reader) (io.ReadCloser, error) {
	return io.NopCloser(bzip2.NewReader(r)), nil
}
