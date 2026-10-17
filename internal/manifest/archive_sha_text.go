package manifest

import (
	"fmt"
	"os"
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
	lines := strings.Split(string(src), "\n")
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
		shaIdx, insertAfter, indent, err := locateArchiveEntry(lines, u.Version, u.OS, u.Arch)
		if err != nil {
			missed = append(missed, fmt.Sprintf("%s/%s/%s: %v", u.Version, u.OS, u.Arch, err))
			continue
		}
		if shaIdx >= 0 {
			if strings.TrimSpace(lines[shaIdx]) == "sha256: "+u.SHA256 {
				continue // 已是目标值（幂等；不计入 changed，也不算 missed）
			}
			lines[shaIdx] = indent + "sha256: " + u.SHA256
			changed++
			continue
		}
		if insertAfter < 0 {
			missed = append(missed, fmt.Sprintf("%s/%s/%s: no anchor line to insert after", u.Version, u.OS, u.Arch))
			continue
		}
		// 缺 sha256 行 → 在锚点行后插入（锚点为该归档项的 url: 或 arch: 行），
		// 行尾风格跟随锚点行，保持整文件一致。
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
	return []byte(b.String()), changed, missed, nil
}

// locateArchiveEntry 定位某归档项的 sha256 行。
// 返回：sha256 行（-1 表示缺）、插入锚点行、字段缩进。
func locateArchiveEntry(lines []string, version, osName, arch string) (shaIdx, insertAfter int, indent string, err error) {
	verRe := regexp.MustCompile(`^(\s*)-\s*version:\s*["']?` + regexp.QuoteMeta(version) + `["']?\s*$`)
	osRe := regexp.MustCompile(`^(\s*)-\s*os:\s*["']?` + regexp.QuoteMeta(osName) + `["']?\s*$`)
	archRe := regexp.MustCompile(`^\s*arch:\s*["']?` + regexp.QuoteMeta(arch) + `["']?\s*$`)
	anyVerRe := regexp.MustCompile(`^\s*-\s*version:`)
	topKeyRe := regexp.MustCompile(`^\S`)

	// 1) 找 version 块
	verStart, verIndent := -1, 0
	for i, l := range lines {
		if m := verRe.FindStringSubmatch(l); m != nil {
			verStart, verIndent = i, len(m[1])
			break
		}
	}
	if verStart < 0 {
		return -1, -1, "", fmt.Errorf("version block not found")
	}
	// 块结束 = 下一个**同缩进**的 `- version:`，或回退到顶层键（缩进 < verIndent）。
	// 后者是第四轮复核补的边界：若版本列表之后还有别的顶层段（remote:/latest: 等），
	// 而那段里恰好有 `- os:` 形式的列表项，仅靠"下一个 version"会把搜索范围扩到该段。
	verEnd := len(lines)
	for i := verStart + 1; i < len(lines); i++ {
		line := lines[i]
		if topKeyRe.MatchString(line) {
			verEnd = i
			break
		}
		if m := anyVerRe.FindStringSubmatch(line); m != nil {
			trimmed := strings.TrimLeft(line, " ")
			if len(line)-len(trimmed) == verIndent {
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
		// 该归档项必须声明目标 arch，并定位 sha256 / 插入锚点。
		//
		// 第四轮复核加固：字段匹配必须**限定在该归档项自身的字段缩进**上，否则归档项里
		// 的嵌套列表（例如 install_args: 下的条目、或嵌套映射里的 url:/sha256:）会被
		// 误当成目标字段——那会把哈希写到错误的层级。
		hasArch, sha, anchor := false, -1, -1
		fieldIndent := ""
		for j := i + 1; j < itemEnd; j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
			if fieldIndent != "" && len(indent) != len(fieldIndent) {
				continue // 更深/更浅的层级都不是本归档项的字段
			}
			switch {
			case archRe.MatchString(l):
				hasArch = true
				if fieldIndent == "" {
					fieldIndent = indent
				}
				if anchor < 0 {
					anchor = j
				}
			case strings.HasPrefix(strings.TrimSpace(l), "url:"):
				if fieldIndent == "" {
					fieldIndent = indent
				}
				anchor = j // url 之后插 sha256 更贴近既有排版
			case strings.HasPrefix(strings.TrimSpace(l), "sha256:"):
				if fieldIndent == "" {
					fieldIndent = indent
				}
				sha = j
			}
		}
		if !hasArch {
			continue
		}
		if fieldIndent == "" {
			fieldIndent = strings.Repeat(" ", itemIndent+2)
		}
		return sha, anchor, fieldIndent, nil
	}
	return -1, -1, "", fmt.Errorf("archive entry not found for %s/%s", osName, arch)
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
	return changed, missed, nil
}

// SetLatestInLines 在 YAML 文本里把顶层 `latest:` 的值改成 newLatest，返回新内容
// 与是否发生改动（第七轮审计 F-29 的注释保全部分）。
//
// 为什么需要它：`xdev refresh` 原先用 manifest.MarshalTool **整文件重写**来更新
// latest，会把用户手写 manifest 里的**全部注释**抹掉。这里只做文本级外科替换——
// 命中顶层 `latest:` 行（缩进为 0，避免误改某个 version 条目下的同名字段）时
// 就地替换值并保留行尾注释；未命中则插入一行。
//
// 与 SetArchiveSHA256InLines 同源的思路：能用文本级改写就不整文件重排。
//
// 第八轮审计自纠（本函数是第七轮的产物，被第八轮探针抓出三个真实缺陷）：
//  1. **CRLF 文件被改成混合行尾**：旧实现 `strings.Split(text, "\n")` + `Join`
//     会把命中行的 `\r` 丢掉，产出 `…\r\n latest: 2.0.0 \n …` 的混合行尾
//     （实测 `"# comment\r\nname: demo\r\nlatest: 2.0.0\nversions: []\r\n"`）。
//     现改为**逐行保留原始行尾**。
//  2. **追加到第 0 行会破坏 YAML 文档结构**：当文件以 `---`（文档起始标记）或
//     `%YAML` 指令开头时，把 `latest:` 前置会产生 `latest: 2.0.0\n---\nname: …`
//     —— 新键落进一个**独立文档**。现改为插入到「前导块之后」（BOM / 指令 /
//     `---` / 注释 / 空行之后）。
//  3. **BOM 被挤到文件中间**：`\ufeffname: demo` 会被改成
//     `latest: 2.0.0\n\ufeffname: demo`，使 UTF-8 BOM 出现在字节 0 之外。
//     现在含 BOM 的首行永不被越过。
func SetLatestInLines(src []byte, newLatest string) ([]byte, bool) {
	if newLatest == "" {
		return src, false
	}
	lines, ends := splitLinesKeepEnds(src)

	for i, line := range lines {
		// 顶层 latest：行首无缩进，且不是注释行。
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		// 第八轮审计 V2：YAML 允许 `latest :`（冒号前有空格）与 `"latest":` /
		// `'latest':`（键加引号）等等价写法。旧实现只认字面前缀 "latest:"，于是
		// 这些写法会被当成「没有顶层 latest」→ 走到插入分支 → 产出**重复键**：
		//   "name: demo\nlatest : 1.0.0\n" → "latest: 2.0.0\nname: demo\nlatest : 1.0.0\n"
		// yaml.Unmarshal 报 `mapping key "latest" already defined`，文件损坏。
		// 现在按「解析出键名」的方式匹配，所有等价写法都就地改写。
		key, rest, ok := splitTopLevelKey(line)
		if !ok || key != "latest" {
			continue
		}
		// 分离「值」与「行尾注释」：找到 '#'，把其**前面的整段空白**一并归入
		// 注释部分，从而逐字节保留 "value  # note" 的双空格写法。
		value, comment := rest, ""
		if hash := strings.Index(rest, "#"); hash >= 0 {
			cut := hash
			for cut > 0 && (rest[cut-1] == ' ' || rest[cut-1] == '\t') {
				cut--
			}
			value, comment = rest[:cut], rest[cut:]
		}
		// 幂等：现值（含被引号包裹的写法）已等于目标则不改动。
		if unquoteYAML(strings.TrimSpace(value)) == newLatest {
			return src, false
		}
		// 保留行首的 UTF-8 BOM（它属于文件级标记，改写键值时不能丢）。
		prefix := ""
		if strings.HasPrefix(line, "\ufeff") {
			prefix = "\ufeff"
		}
		// 第八轮审计 V3/V4：写回的值必须做 YAML 标量处理 ——
		//  V3：原样拼接特殊字符会产出**非法 YAML**（实测 `a: b` → mapping values
		//      not allowed；`*glob` → unknown anchor；`- dash` → block sequence
		//      entries not allowed；`[bracket`、`"quote` 同理）。
		//  V4：原值带引号时丢掉引号是格式回归（且可能改变解析结果，如 `"1.0"` 与
		//      `1.0` 在 yaml 里类型不同）。
		// 规则：原值有引号 → 沿用同一引号风格；原值无引号但新值需要引号 → 加双引号。
		lines[i] = prefix + "latest: " + yamlScalarFor(newLatest, strings.TrimSpace(value)) + comment
		return joinLinesKeepEnds(lines, ends), true
	}

	// 没有顶层 latest：插入到「前导块之后」，避免破坏文档结构或挤走 BOM。
	insertAt := 0
	for insertAt < len(lines) {
		raw := lines[insertAt]
		// BOM 属文件级标记，必须留在字节 0 —— 不能把新键插到它前面。
		if insertAt == 0 && strings.HasPrefix(raw, "\ufeff") {
			insertAt++
			continue
		}
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "%") || t == "---" {
			insertAt++
			continue
		}
		break
	}

	// 新增行沿用邻近行的行尾风格（CRLF 文件保持 CRLF）。
	nl := "\n"
	switch {
	case insertAt > 0:
		nl = ends[insertAt-1]
	case len(ends) > 0:
		nl = ends[0]
	}
	if nl == "" {
		nl = "\n"
	}

	// 第八轮审计 R8-1（P1，本修复自身引入的回归）：前导块的**最后一行没有换行符**
	// 时（例如文件整体就是一行注释 `# c1`，或最后一行是注释且无末尾换行），
	// `ends[insertAt-1] == ""`。旧写法把 `nl` 只当作**新行自己的**行尾，从不作为
	// 前导分隔符插入 → 新行被直接粘在注释尾部：
	//   "# c1" → "# c1latest: 2.0\n"（YAML 里 latest 键根本不存在！）
	// 而 refresh 仍按 changed=true 打印 "latest: 旧 -> 新"，即**谎报成功且写坏文件**。
	// 现在：前导块末尾缺换行时，先补一个分隔符再写新行。
	outLines := make([]string, 0, len(lines)+2)
	outEnds := make([]string, 0, len(ends)+2)
	outLines = append(outLines, lines[:insertAt]...)
	outEnds = append(outEnds, ends[:insertAt]...)
	if insertAt > 0 && (insertAt-1 >= len(ends) || ends[insertAt-1] == "") {
		// 前导块末行没有行尾：补一个空行作为分隔，保证 latest 独立成行。
		outLines = append(outLines, "")
		outEnds = append(outEnds, nl)
	}
	outLines = append(outLines, "latest: "+newLatest)
	outEnds = append(outEnds, nl)
	outLines = append(outLines, lines[insertAt:]...)
	outEnds = append(outEnds, ends[insertAt:]...)
	return joinLinesKeepEnds(outLines, outEnds), true
}

// splitTopLevelKey 从一行 YAML 顶层映射条目里解析出「键名」与「冒号之后的原文」。
//
// 支持 YAML 的等价写法（第八轮审计 V2）：
//
//	latest: 1.0.0      → ("latest", " 1.0.0")
//	latest : 1.0.0     → ("latest", " 1.0.0")
//	"latest": 1.0.0    → ("latest", " 1.0.0")
//	'latest': 1.0.0    → ("latest", " 1.0.0")
//
// 非映射条目（无冒号、是列表项 `- x`、或键为空）返回 ok=false。
func splitTopLevelKey(line string) (key, rest string, ok bool) {
	s := line
	// 首行的 UTF-8 BOM 属于文件级标记，不是键名的一部分。
	if strings.HasPrefix(s, "\ufeff") {
		s = strings.TrimPrefix(s, "\ufeff")
	}
	// 列表项不是映射键。
	if strings.HasPrefix(strings.TrimLeft(s, " \t"), "- ") || strings.TrimSpace(s) == "-" {
		return "", "", false
	}
	// 找到不在引号内的冒号。
	inSingle, inDouble := false, false
	colon := -1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if !inSingle && !inDouble {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon < 0 {
		return "", "", false
	}
	rawKey := strings.TrimSpace(s[:colon])
	if rawKey == "" {
		return "", "", false
	}
	return unquoteYAML(rawKey), s[colon+1:], true
}

// yamlScalarFor 为写回 `latest:` 的值生成安全的 YAML 标量文本（第八轮审计 V3/V4）。
//
// oldRaw 是该键**原有**的值原文（未去引号），用于沿用原有引号风格：
//   - 原值被单引号包裹 → 新值也用单引号（内部单引号按 YAML 规则双写转义）；
//   - 原值被双引号包裹 → 新值也用双引号（内部反斜杠/双引号转义）；
//   - 原值无引号 → 新值本身不需要引号就裸写，否则加双引号。
//
// needsYAMLQuoting 判定裸写是否会产出非法 YAML 或改变语义。
func yamlScalarFor(newValue, oldRaw string) string {
	switch {
	case len(oldRaw) >= 2 && oldRaw[0] == '\'' && oldRaw[len(oldRaw)-1] == '\'':
		return "'" + strings.ReplaceAll(newValue, "'", "''") + "'"
	case len(oldRaw) >= 2 && oldRaw[0] == '"' && oldRaw[len(oldRaw)-1] == '"':
		esc := strings.ReplaceAll(newValue, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		return `"` + esc + `"`
	}
	if !needsYAMLQuoting(newValue) {
		return newValue
	}
	esc := strings.ReplaceAll(newValue, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	return `"` + esc + `"`
}

// needsYAMLQuoting 报告裸写该标量是否会产出非法/歧义 YAML。
//
// 覆盖：空串、首尾空白、YAML 指示符（`- ? : , [ ] { } # & * ! | > ' " % @ \“）、
// 含 `: `（映射歧义）或 ` #`（注释歧义）、以及看起来像数字/布尔/null 的串
// （裸写会被解析成非字符串类型）。
func needsYAMLQuoting(v string) bool {
	if v == "" {
		return true
	}
	if strings.TrimSpace(v) != v {
		return true
	}
	if strings.ContainsAny(v, "\n\r\t") {
		return true
	}
	switch v[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '#', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
		return true
	}
	if strings.Contains(v, ": ") || strings.HasSuffix(v, ":") {
		return true
	}
	if strings.Contains(v, " #") {
		return true
	}
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		return true
	}
	return false
}

// unquoteYAML 去掉值两侧成对的单/双引号，用于幂等比较。
func unquoteYAML(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// splitLinesKeepEnds 按 '\n' 切分并**逐行保留原始行尾**（"\n" 或 "\r\n"），
// 使改写后的文件不会出现混合行尾。
func splitLinesKeepEnds(src []byte) (lines, ends []string) {
	s := string(src)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			if s != "" {
				lines = append(lines, s)
				ends = append(ends, "")
			}
			return
		}
		line, end := s[:i], "\n"
		if strings.HasSuffix(line, "\r") {
			line, end = line[:len(line)-1], "\r\n"
		}
		lines = append(lines, line)
		ends = append(ends, end)
		s = s[i+1:]
	}
}

// joinLinesKeepEnds 是 splitLinesKeepEnds 的逆操作。
func joinLinesKeepEnds(lines, ends []string) []byte {
	var b strings.Builder
	for i := range lines {
		b.WriteString(lines[i])
		if i < len(ends) {
			b.WriteString(ends[i])
		}
	}
	return []byte(b.String())
}

// UpdateLatestInFile 在文件上做一次「加锁 → 文本级改 latest → 原子写」。
// 返回是否发生改动。用于 `xdev refresh` 在保住注释的前提下更新 latest（F-29）。
func UpdateLatestInFile(path, newLatest string) (bool, error) {
	lock, lockErr := util.AcquireFileLock(path+".lock", 30*time.Second)
	if lockErr != nil {
		return false, fmt.Errorf("manifest lock %s.lock: %w", path, lockErr)
	}
	defer lock.Release()

	src, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	out, changed := SetLatestInLines(src, newLatest)
	if !changed {
		return false, nil
	}
	if err := util.AtomicWriteFile(path, out, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
