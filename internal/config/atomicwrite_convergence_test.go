package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// 票 31（P1-24 收敛核验）：AtomicWriteFile 调用收敛 + 固定 tmp 中间文件清理
// ---------------------------------------------------------------------------

// repoRoot 返回仓库根（测试 CWD = internal/config）。
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s: %v", root, err)
	}
	return root
}

// fixedTmpExceptions 记录「仍使用固定 <path>.tmp 中间文件」的已知点。
//
// 票 31 的白名单约束：本票只收敛 internal/config（writeHomeCache）与
// internal/update/policy.go 两处；其余同型克隆不在本 agent 的文件白名单内，
// 只在票卡报告里列出。该 map 是「允许存在」清单——新增任何未登记点都会让本测试变红。
//
// 第九轮审计 A-10 更新：**P1-24 本体 `internal/util/fs.go`（AtomicWriteFile）
// 已完成 CreateTemp 迁移**，因此从本清单移除。它是所有调用点的共享底层
// （config/manifest/discovery/isolator 都经它落盘），移除后由
// internal/util/fs_test.go 的并发守卫直接钉住（16 写者 × 40 轮：固定 .tmp 下实测
// 610 次失败/污染，迁移后为 0）。
var fixedTmpExceptions = map[string]string{
	"internal/installer/mirror_selector.go": "MirrorHealthCache.Save 克隆（internal/installer 域，白名单外，待迁移 CreateTemp）",
	"toolsHashFullCheck/progress.go":        "Progress.Save 克隆（根级工具，白名单外，待迁移 CreateTemp）",
	"internal/downloader/downloader.go":     "下载 sidecar 的 .tmp 中间文件（internal/downloader 域，票 21/23 负责）",
}

// TC-31-1 grep 守卫：除登记例外外，仓库内不得出现固定 "<path>.tmp" 中间文件。
//
// 判定口径：生产代码（非 _test.go，且剔除行注释后的代码文本）里出现
// `+ ".tmp"` 拼接即为固定中间文件；唯名临时文件
// （os.CreateTemp(dir, base+".tmp-*")）不匹配。
func TestNoFixedTmpIntermediateFiles(t *testing.T) {
	root := repoRoot(t)
	fixedRe := regexp.MustCompile(`\+\s*"\.tmp"`)

	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "build", "vendor", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !fixedRe.Match(stripLineComments(data)) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		hits = append(hits, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	for _, h := range hits {
		if reason, ok := fixedTmpExceptions[h]; ok {
			t.Logf("已知例外（票 31 报告项，白名单外）: %s — %s", h, reason)
			continue
		}
		t.Errorf("%s 使用了固定 \"<path>.tmp\" 中间文件（P1-24 回归）：并发写者会互相截断，请改用 os.CreateTemp 唯名临时文件", h)
	}

	// 本票收敛的两处克隆必须在例外清单之外（即已迁移）。
	for _, own := range []string{"internal/config/config.go", "internal/update/policy.go"} {
		for _, h := range hits {
			if h == own {
				t.Errorf("%s 仍在使用固定 .tmp（本票应收敛为 CreateTemp 形态）", own)
			}
		}
	}
}

// TC-31-1b P1-24 调用清单收敛核验（8+4）：原子写调用点仍存在，且四克隆中
// 本票负责的两处为 CreateTemp 形态。
func TestAtomicWriteCallSitesConverged(t *testing.T) {
	root := repoRoot(t)

	// 8 个调用点（审计清单；>= 断言以容忍后续新增）
	//
	// 第七轮审计 F-29 更新：internal/cli/refresh.go 的调用点**已按设计消失** ——
	// refresh 原先自己 MarshalTool + AtomicWriteFile 整文件重写，会抹掉用户手写
	// manifest 的全部注释；现改为委托 internal/manifest.UpdateLatestInFile（加锁 →
	// 文本级外科写 → util.AtomicWriteFile），因此原子写仍走收敛路径，只是发生地
	// 从 cli 移到了 manifest 包（该包的清单条目本就覆盖）。这不是放宽断言，而是
	// 把清单同步到一次有意的架构收敛。
	atomicCallSites := map[string]int{
		"internal/config/config.go":        2, // saveLocked + UpdateConfigPaths
		"internal/discovery/scheduler.go":  4, // sniff cache + .bak + manifest 写回
		"internal/manifest/manifest.go":    3,
		"internal/gui/app_custom_tools.go": 1,
	}
	for rel, want := range atomicCallSites {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if got := strings.Count(string(data), "util.AtomicWriteFile("); got < want {
			t.Errorf("%s: util.AtomicWriteFile 调用 = %d, want >= %d（P1-24 收敛清单）", rel, got, want)
		}
	}

	// 4 个同型克隆：本票负责的两处必须是 CreateTemp 形态。
	clones := map[string]string{
		"internal/config/config.go": "writeHomeCache",
		"internal/update/policy.go": "writeJSONAtomic",
	}
	for rel, fn := range clones {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(data), "os.CreateTemp(") {
			t.Errorf("%s（%s）未使用 os.CreateTemp 唯名临时文件", rel, fn)
		}
		if !strings.Contains(string(data), "tmp.Sync()") {
			t.Errorf("%s（%s）缺少 Sync（崩溃时可能发布空/半截文件）", rel, fn)
		}
	}
}

// TC-31-1c P1-24 行为守卫：100 goroutine 并发写同一路径 + 8 个并发读者循环
// 校验内容——读到的内容必须始终是某一次完整写入（变长负载 + 结构自校验）。
//
// 固定 "<path>.tmp" 版本下并发写者共享同一中间文件：rename 会把别的写者的
// 半截内容发布成目标 → 读者检出撕裂（长度/字符混合）。CreateTemp 版本下
// 目标内容恒为某一次完整写入（rename 原子发布），读者永不撕裂。
//
// 注：不把「写者 rename 失败」计入失败——Windows 上 os.ReadFile 以
// O_CLOEXEC 打开（不含 FILE_SHARE_DELETE），并发读者会让 REPLACE_EXISTING
// 偶发 ERROR_ACCESS_DENIED（与中间文件方案无关），因此写者侧只记录不判定。
func TestWriteFileCreateTemp_ConcurrentWritersNoTornReads(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "home.cache")

	const writers = 100
	payload := func(i int) []byte {
		body := bytes.Repeat([]byte{byte('a' + i%26)}, 64+i*8)
		return append([]byte(fmt.Sprintf("[%03d]", i)), body...)
	}
	// 校验："[NNN]" + 单一字符重复，长度必须与 NNN 对应。
	valid := func(b []byte) bool {
		if len(b) < 6 || b[0] != '[' || b[4] != ']' {
			return false
		}
		i, err := strconv.Atoi(string(b[1:4]))
		if err != nil || i < 0 || i >= writers {
			return false
		}
		if len(b) != 5+64+i*8 {
			return false
		}
		c := b[5]
		for _, x := range b[5:] {
			if x != c {
				return false
			}
		}
		return true
	}

	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	var mu sync.Mutex
	var torn []string
	var reads int
	for r := 0; r < 8; r++ {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(target)
				if err != nil {
					continue // 首次写入前不存在 / 被并发替换：跳过
				}
				mu.Lock()
				reads++
				if !valid(data) && len(torn) < 5 {
					torn = append(torn, fmt.Sprintf("len=%d head=%q", len(data), head(data, 12)))
				}
				mu.Unlock()
			}
		}()
	}

	var wg sync.WaitGroup
	var writeErrs int
	var errMu sync.Mutex
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := writeFileCreateTemp(target, payload(i), 0o644); err != nil {
				errMu.Lock()
				writeErrs++
				errMu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	close(stop)
	readerWG.Wait()

	mu.Lock()
	tornCopy, readCount := append([]string(nil), torn...), reads
	mu.Unlock()
	errMu.Lock()
	errs := writeErrs
	errMu.Unlock()

	if readCount == 0 {
		t.Error("读者没有完成任何一次读取（测试无效）")
	}
	if len(tornCopy) > 0 {
		t.Errorf("读者读到撕裂内容（固定 tmp 中间文件互相截断的典型症状）: %v", tornCopy)
	}
	final, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !valid(final) {
		t.Errorf("最终内容不是完整写入: len=%d head=%q", len(final), head(final, 12))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("并发写残留临时文件: %s", e.Name())
		}
	}
	// 写者错误只记录（见函数注释：并发读者的 O_CLOEXEC 打开会令
	// REPLACE_EXISTING 偶发 ACCESS_DENIED，与中间文件方案无关）。
	if errs > 0 {
		t.Logf("note: %d/%d 次并发 rename 因读写竞争返回错误（已在实现内重试）", errs, writers)
	}
}

func head(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return string(b[:n])
}

// stripLineComments 去掉每行的 // 之后内容（守卫只针对代码，不针对注释文案）。
func stripLineComments(src []byte) []byte {
	lines := bytes.Split(src, []byte("\n"))
	for i, l := range lines {
		if idx := bytes.Index(l, []byte("//")); idx >= 0 {
			lines[i] = l[:idx]
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

// TC-31-1c2 确定性守卫：实现不得依赖固定 "<path>.tmp" 中间文件名。
//
// 占位技巧：把固定中间文件名 target+".tmp" 预建为**目录**——唯名临时文件
// （os.CreateTemp(dir, base+".tmp-*")）完全不受影响；而 v1 的
// 「写 target+".tmp" 再 rename」实现在此处必然失败（OpenFile 打不开目录）。
// 相比并发时序探测，这条判定是确定性的。
func TestWriteFileCreateTemp_DoesNotUseFixedTmpName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "home.cache")
	if err := os.MkdirAll(target+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}

	if err := writeFileCreateTemp(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("writeFileCreateTemp 依赖固定 <path>.tmp 中间文件（被占位即失败）: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("target content = %q, want payload", string(data))
	}
	if fi, err := os.Stat(target + ".tmp"); err != nil || !fi.IsDir() {
		t.Errorf("占位目录被破坏: fi=%v err=%v", fi, err)
	}
}

// TC-31-1d writeHomeCache 走 CreateTemp 路径且不残留中间文件
// （票 31 四克隆之一，本票收敛点）。
func TestWriteHomeCache_NoFixedTmpResidue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)        // Unix: os.UserHomeDir
	t.Setenv("USERPROFILE", home) // Windows: os.UserHomeDir
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XMDEV_HOME", "")

	prevHome := homeVal
	homeVal = "" // 强制 fallback 路径（LOCALAPPDATA / ~/.xdev），避免污染真实缓存
	defer func() { homeVal = prevHome }()

	cachePath := fallbackCachePath()
	if !strings.HasPrefix(filepath.Clean(cachePath), filepath.Clean(home)) {
		t.Fatalf("cache path %q escaped the temp home %q", cachePath, home)
	}

	writeHomeCache(home)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read home cache: %v", err)
	}
	if !strings.HasPrefix(string(data), home+"\n") {
		t.Errorf("cache content = %q, want prefix %q", string(data), home+"\n")
	}
	entries, err := os.ReadDir(filepath.Dir(cachePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("home cache 残留临时文件: %s", e.Name())
		}
	}
}
