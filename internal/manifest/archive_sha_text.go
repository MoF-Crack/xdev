package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/util"
)

// ArchiveSHAUpdate 是一次"归档哈希回填"请求（fill-sha256 用）。
type ArchiveSHAUpdate struct {
	Version string
	OS      string
	Arch    string
	SHA256  string
}

// SetArchiveSHA256InLines 以**文本级**方式更新若干归档条目的 sha256 字段。
//
// 为什么不能用 MarshalTool（本批复核的实证结论）：yaml.Unmarshal → yaml.Marshal 的
// 往返会**丢弃全部注释并重排格式**。实测把仓库 52 个 manifest 全走一遍该路径：
// 5990 行 → 6989 行、**220 行注释归零**、最坏的文件只有 5% 的行原样保留。对"重填 163 个
// 哈希"这种只应改动若干行的操作，那意味着 PR 完全不可评审（哈希变化淹没在格式噪声里），
// 并且会把 manifest 里的说明性注释（上游改名依据、A-03 依赖评估、安装策略等）一并抹掉。
//
// 因此这里只做文本手术：
//   - 定位 <version> 块（按 `- version:` 的缩进判断块的起止）；
//   - 在块内定位 `- os: <os>` 且含 `arch: <arch>` 的归档项；
//   - 只替换该项里的 `sha256:` 一行；缺该行时按同级缩进插入一行；
//   - 其余一切（注释、引号风格、缩进宽度、字段顺序、行尾 CRLF/LF）原样保留。
//
// 返回实际改动的条目数与错误。定位不到某条时**不猜**：计入未命中（调用方负责报错），
// 因为静默跳过会让"填了却没用上"变成看不见的缺口。
func SetArchiveSHA256InLines(src []byte, updates []ArchiveSHAUpdate) ([]byte, int, []string, error) {
	text := string(src)
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	lines := strings.Split(text, "\n")
	// 记下每行是否带 \r，改写时原样保留行尾风格。
	hadCR := make([]bool, len(lines))
	for i, l := range lines {
		if strings.HasSuffix(l, "\r") {
			hadCR[i] = true
			lines[i] = strings.TrimSuffix(l, "\r")
		}
	}

	changed := 0
	var missed []string
	for _, u := range updates {
		idx, shaIdx, insertAfter, indent, err := locateArchiveEntry(lines, u.Version, u.OS, u.Arch)
		if err != nil {
			missed = append(missed, fmt.Sprintf("%s/%s/%s: %v", u.Version, u.OS, u.Arch, err))
			continue
		}
		_ = idx
		if shaIdx >= 0 {
			if strings.TrimSpace(lines[shaIdx]) == "sha256: "+u.SHA256 {
				continue // 已是目标值（幂等）
			}
			lines[shaIdx] = indent + "sha256: " + u.SHA256
			changed++
			continue
		}
		// 缺 sha256 行 → 在锚点行后插入（锚点为该归档项的 url: 或 arch: 行）
		insertAt := insertAfter + 1
		lines = append(lines[:insertAt], append([]string{indent + "sha256: " + u.SHA256}, lines[insertAt:]...)...)
		hadCR = append(hadCR[:insertAt], append([]bool{hadCR[insertAfter]}, hadCR[insertAt:]...)...)
		changed++
	}

	if changed == 0 {
		return src, 0, missed, nil
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(l)
		if i < len(lines)-1 {
			if hadCR[i] {
				b.WriteString("\r")
			}
			b.WriteString("\n")
		}
	}
	_ = eol
	return []byte(b.String()), changed, missed, nil
}

// locateArchiveEntry 定位某归档项的 sha256 行。
// 返回：归档项起始行、sha256 行（-1 表示缺）、插入锚点行、字段缩进（含 null 语义）。
func locateArchiveEntry(lines []string, version, osName, arch string) (start, shaIdx, insertAfter int, indent string, err error) {
	verRe := regexp.MustCompile(`^(\s*)-\s*version:\s*["']?` + regexp.QuoteMeta(version) + `["']?\s*$`)
	osRe := regexp.MustCompile(`^(\s*)-\s*os:\s*["']?` + regexp.QuoteMeta(osName) + `["']?\s*$`)
	archRe := regexp.MustCompile(`^\s*arch:\s*["']?` + regexp.QuoteMeta(arch) + `["']?\s*$`)
	anyVerRe := regexp.MustCompile(`^\s*-\s*version:`)

	// 1) 找 version 块
	verStart, verIndent := -1, 0
	for i, l := range lines {
		if m := verRe.FindStringSubmatch(l); m != nil {
			verStart, verIndent = i, len(m[1])
			break
		}
	}
	if verStart < 0 {
		return 0, 0, 0, "", fmt.Errorf("version block not found")
	}
	verEnd := len(lines)
	for i := verStart + 1; i < len(lines); i++ {
		if m := anyVerRe.FindStringSubmatch(lines[i]); m != nil {
			// 同缩进的 `- version:` 才是下一个版本块
			trimmed := strings.TrimLeft(lines[i], " ")
			if len(lines[i])-len(trimmed) == verIndent {
				verEnd = i
				break
			}
		}
	}

	// 2) 在块内找归档项
	for i := verStart + 1; i < verEnd; i++ {
		m := osRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		itemIndent := len(m[1])
		itemEnd := verEnd
		for j := i + 1; j < verEnd; j++ {
			if l := lines[j]; strings.HasPrefix(l, strings.Repeat(" ", itemIndent)+"- ") {
				itemEnd = j
				break
			}
		}
		// 该归档项必须声明目标 arch，并定位 sha256 / 插入锚点
		hasArch, sha, anchor := false, -1, -1
		fieldIndent := ""
		for j := i + 1; j < itemEnd; j++ {
			l := lines[j]
			switch {
			case archRe.MatchString(l):
				hasArch = true
				if fieldIndent == "" {
					fieldIndent = l[:len(l)-len(strings.TrimLeft(l, " "))]
				}
				if anchor < 0 {
					anchor = j
				}
			case strings.HasPrefix(strings.TrimSpace(l), "url:"):
				if fieldIndent == "" {
					fieldIndent = l[:len(l)-len(strings.TrimLeft(l, " "))]
				}
				anchor = j // url 之后插 sha256 更贴近既有排版
			case strings.HasPrefix(strings.TrimSpace(l), "sha256:"):
				sha = j
			}
		}
		if !hasArch {
			continue
		}
		if fieldIndent == "" {
			fieldIndent = strings.Repeat(" ", itemIndent+2)
		}
		return i, sha, anchor, fieldIndent, nil
	}
	return 0, 0, 0, "", fmt.Errorf("archive entry not found for %s/%s", osName, arch)
}

// UpdateArchiveSHA256InFile 在文件上应用若干归档哈希更新：加锁 → 文本级改写 → 原子写。
//
// 加锁（<path>.lock，与票 23 的 UpdateArchiveSHA256 / sniff 合并同一把锁）解决的是另一处
// 复核发现：fill 原先是"整文件重写且不加锁"，与并发的嗅探合并互为无锁竞争（后写者覆盖、
// 固定 .tmp 名还可能互相截断）。返回实际改动条目数与未命中说明。
func UpdateArchiveSHA256InFile(path string, updates []ArchiveSHAUpdate) (int, []string, error) {
	if len(updates) == 0 {
		return 0, nil, nil
	}
	lock, lockErr := util.AcquireFileLock(path+".lock", 30*time.Second)
	if lockErr != nil {
		return 0, nil, fmt.Errorf("manifest lock %s.lock: %w", path, lockErr)
	}
	defer lock.Release()

	src, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, fmt.Errorf("read %s: %w", path, err)
	}
	out, changed, missed, err := SetArchiveSHA256InLines(src, updates)
	if err != nil {
		return 0, nil, err
	}
	if changed == 0 {
		return 0, missed, nil
	}
	if err := util.AtomicWriteFile(path, out, 0o644); err != nil {
		return 0, nil, fmt.Errorf("write %s: %w", path, err)
	}
	_ = filepath.Dir(path)
	return changed, missed, nil
}
