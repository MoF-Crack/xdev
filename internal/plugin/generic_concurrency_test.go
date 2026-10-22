package plugin

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/xmdemt/xdev/internal/manifest"
)

// ──────────────────────────────────────────────────────
// 票 24 守卫测试（CT4 / P1-16 / P2-07）：Generic 的 SetManifest 与 13 个
// 只读方法并发交错时不 panic、且每次读到的值必须来自某一个完整 manifest
// （不得是两次替换的混合值）。
//
// 本机 `go test -race` 不可用（唯一 MinGW 8.1.0 与 Go 1.26 race 运行时不
// 兼容），因此 CT1 原计划的 -race 门在本机以「goroutine 交错 + 多轮循环」
// 替代；有 race 运行时的环境应追加 `go test -race ./internal/plugin/`。
// ──────────────────────────────────────────────────────

func ctManifest(name, ver, url string) *manifest.Tool {
	return &manifest.Tool{
		Name:        name,
		DisplayName: "Display " + name,
		Description: "desc " + name,
		Homepage:    "https://example.com/" + name,
		Category:    "cat-" + name,
		Shims:       []string{name, name + "ctl"},
		EnvVars:     map[string]string{"HOME_" + name: "{{install}}"},
		Latest:      ver,
		VersionList: []manifest.Version{{
			Version:  ver,
			Archives: []manifest.Archive{{OS: "any", Arch: "any", URL: url, Format: "zip"}},
		}},
	}
}

func TestGeneric_ConcurrentSetManifestAndReads(t *testing.T) {
	// 两个各自自洽的 manifest 交替替换：任何一次读取的结果都必须完整属于
	// alpha 或 beta 之一。
	alpha := ctManifest("alpha", "1.0.0", "https://example.com/alpha-1.0.0.zip")
	beta := ctManifest("beta", "2.0.0", "https://example.com/beta-2.0.0.zip")

	g := NewGeneric(alpha)

	names := map[string]bool{"alpha": true, "beta": true}
	displays := map[string]bool{"Display alpha": true, "Display beta": true}
	descs := map[string]bool{"desc alpha": true, "desc beta": true}
	homepages := map[string]bool{"https://example.com/alpha": true, "https://example.com/beta": true}
	categories := map[string]bool{"cat-alpha": true, "cat-beta": true}
	shimSets := map[string]bool{"alpha,alphactl": true, "beta,betactl": true}
	versions := map[string]bool{"1.0.0": true, "2.0.0": true}

	const rounds = 400
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []string
	)
	fail := func(format string, args ...any) {
		mu.Lock()
		errs = append(errs, fmt.Sprintf(format, args...))
		mu.Unlock()
	}

	// 写方：反复 SetManifest 换指针（reloadCatalog 嗅探合并后的真实写路径）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			if i%2 == 0 {
				g.SetManifest(beta)
			} else {
				g.SetManifest(alpha)
			}
			runtime.Gosched()
		}
	}()

	// 读方：13 个只读方法并发交错。每个读方至少跑 minIters 轮，
	// 保证即使写方先结束也完成足够的交错次数。
	const minIters = 200
	check := func(method string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < minIters; i++ {
				if err := fn(); err != nil {
					fail("%s: %v", method, err)
					return
				}
			}
		}()
	}

	check("Name", func() error {
		if !names[g.Name()] {
			return fmt.Errorf("Name=%q 不属于任何已知 manifest", g.Name())
		}
		return nil
	})
	check("DisplayName", func() error {
		if !displays[g.DisplayName()] {
			return fmt.Errorf("DisplayName=%q 不属于任何已知 manifest", g.DisplayName())
		}
		return nil
	})
	check("Description", func() error {
		if !descs[g.Description()] {
			return fmt.Errorf("Description=%q 不属于任何已知 manifest", g.Description())
		}
		return nil
	})
	check("Homepage", func() error {
		if !homepages[g.Homepage()] {
			return fmt.Errorf("Homepage=%q 不属于任何已知 manifest", g.Homepage())
		}
		return nil
	})
	check("Category", func() error {
		if !categories[g.Category()] {
			return fmt.Errorf("Category=%q 不属于任何已知 manifest", g.Category())
		}
		return nil
	})
	// Manifest(): 快照自身必须自洽（同一份 manifest 的字段成套出现）。
	check("Manifest", func() error {
		m := g.Manifest()
		if m == nil {
			return fmt.Errorf("Manifest() 返回 nil")
		}
		if !names[m.Name] {
			return fmt.Errorf("snapshot.Name=%q 不属于任何已知 manifest", m.Name)
		}
		if m.DisplayName != "Display "+m.Name || m.Category != "cat-"+m.Name {
			return fmt.Errorf("snapshot 字段混合: name=%q display=%q category=%q", m.Name, m.DisplayName, m.Category)
		}
		if len(m.VersionList) != 1 || m.Latest != m.VersionList[0].Version {
			return fmt.Errorf("snapshot 版本字段不自洽: latest=%q list=%v", m.Latest, m.VersionList)
		}
		return nil
	})
	check("Shims", func() error {
		got := strings.Join(g.Shims(), ",")
		if !shimSets[got] {
			return fmt.Errorf("Shims=%q 不属于任何已知 manifest", got)
		}
		return nil
	})
	check("DefaultVersion", func() error {
		if !versions[g.DefaultVersion()] {
			return fmt.Errorf("DefaultVersion=%q 不属于任何已知 manifest", g.DefaultVersion())
		}
		return nil
	})
	check("ResolveVersion", func() error {
		v, err := g.ResolveVersion("latest")
		if err != nil {
			return fmt.Errorf("ResolveVersion(latest) 失败: %v", err)
		}
		if !versions[v] {
			return fmt.Errorf("ResolveVersion=%q 不属于任何已知 manifest", v)
		}
		return nil
	})
	check("ArchiveFor", func() error {
		arch, err := g.ArchiveFor("1.0.0")
		if err != nil {
			// 当前 manifest 为 beta 时 1.0.0 不存在——这是合法结果，
			// 但不允许出现其他错误（如 nil 解引用）。
			if !strings.Contains(err.Error(), "not in manifest") {
				return fmt.Errorf("ArchiveFor 意外错误: %v", err)
			}
			return nil
		}
		if !strings.HasPrefix(arch.URL, "https://example.com/alpha-1.0.0.zip") {
			return fmt.Errorf("ArchiveFor URL=%q 与 1.0.0 不自洽", arch.URL)
		}
		return nil
	})
	check("InstallDir", func() error {
		root := filepath.Join("C:", "root")
		dir, err := g.InstallDir(root, "1.0.0")
		if err != nil {
			return err
		}
		if !strings.HasPrefix(dir, root) {
			return fmt.Errorf("InstallDir=%q 逃出 install root", dir)
		}
		if !strings.Contains(dir, "alpha") && !strings.Contains(dir, "beta") {
			return fmt.Errorf("InstallDir=%q 不属于任何已知 manifest", dir)
		}
		return nil
	})
	check("EnvVars", func() error {
		vars := g.EnvVars("/install/here")
		if len(vars) != 1 {
			return fmt.Errorf("EnvVars 条目数=%d, want 1: %v", len(vars), vars)
		}
		for k, v := range vars {
			if k != "HOME_alpha" && k != "HOME_beta" {
				return fmt.Errorf("EnvVars 键=%q 不属于任何已知 manifest", k)
			}
			if v != "/install/here" {
				return fmt.Errorf("EnvVars 值未插值: %q", v)
			}
		}
		return nil
	})
	check("RequiredPlugin", func() error {
		if got := g.RequiredPlugin(); got != nil {
			return fmt.Errorf("RequiredPlugin=%v, want nil", got)
		}
		return nil
	})

	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("并发 SetManifest + 13 个读方法交错时发现 %d 个问题（示例: %s）", len(errs), errs[0])
	}
	t.Logf("并发交错 %d 轮（写方）+ %d 轮/方法（13 个读方法）无 panic、无混合读",
		rounds, minIters)
}
