// app_clean_cache_metadata_test.go 守卫 F-06（P1）：CleanCache 的 "all"
// 分类必须只删除真正的缓存（归档 / .sha256 / .part），**不得**删除同目录下
// 的用户状态元数据文件。
//
// 缺陷现场：CleanCache 的 case "all" 直接 shouldDelete = true，把 CacheDir
// 里的每一个文件都 os.Remove 掉——favorites.json（收藏）、
// ignored_tools.json（忽略列表）、custom_tools.json（自定义工具）、
// keystore.dat（密钥库）、shim-index.v1、sniff-cache.json 全部被无差别删除，
// 且没有任何提示。对照实现 CleanCacheCategory（app_cache_delete.go）经
// cache_classify.go 的 cacheEntryCategory 分类，元数据落入 "otherFiles"，
// 只有显式请求该分类时才删除。
//
// 修复方向：CleanCache 与 CleanCacheCategory 共用同一实现，"all" 只删
// archives / shaFiles / partFiles（真正的缓存），元数据一律排除。
//
// 宿主安全：全部落在 t.TempDir() 内，不触碰真实 home。
package gui

import (
	"os"
	"path/filepath"
	"testing"
)

// f06MetadataFiles 是 CacheDir 里必须活过 CleanCache("all") 的用户状态文件
// （以及 xdev 自身的派生缓存/索引——它们同样不是可重下的归档）。
var f06MetadataFiles = []string{
	"favorites.json",
	"ignored_tools.json",
	"custom_tools.json",
	"keystore.dat",
	"shim-index.v1",
	"sniff-cache.json",
	"tool_scan_cache.json",
}

// f06CacheFiles 是真正的缓存（可重下/中间态），"all" 必须删除它们。
var f06CacheFiles = []string{
	"go-1.26.5.zip",
	"llvm-22.1.8.tar.xz",
	"java-17.zip.sha256",
	"node-20.tgz.part",
}

func writeF06Files(t *testing.T, dir string, names []string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
}

// TestCleanCacheAll_KeepsUserMetadata（修复前红）：CleanCache("all") 之后
// 用户状态文件与 xdev 派生索引必须仍然存在；真正的缓存必须被删除。
func TestCleanCacheAll_KeepsUserMetadata(t *testing.T) {
	app, tmp := retentionApp(t)
	cacheDir := filepath.Join(tmp, "cache")

	writeF06Files(t, cacheDir, f06MetadataFiles)
	writeF06Files(t, cacheDir, f06CacheFiles)

	if err := app.CleanCache("all"); err != nil {
		t.Fatalf("CleanCache(\"all\"): %v", err)
	}

	// 用户状态：必须存活。
	for _, name := range f06MetadataFiles {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err != nil {
			t.Errorf("CleanCache(\"all\") 删除了用户状态文件 %s（F-06）: %v", name, err)
		}
	}

	// 真缓存：必须被删除（防「修成 no-op」）。
	for _, name := range f06CacheFiles {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); !os.IsNotExist(err) {
			t.Errorf("CleanCache(\"all\") 未删除缓存文件 %s: err=%v", name, err)
		}
	}
}

// TestCleanCache_AllMatchesCleanCacheCategoryAll：CleanCache 与
// CleanCacheCategory 必须共用同一实现——同一份输入下两者的删除集合一致。
func TestCleanCache_AllMatchesCleanCacheCategoryAll(t *testing.T) {
	appA, tmpA := retentionApp(t)
	appB, tmpB := retentionApp(t)

	all := append(append([]string{}, f06MetadataFiles...), f06CacheFiles...)
	writeF06Files(t, filepath.Join(tmpA, "cache"), all)
	writeF06Files(t, filepath.Join(tmpB, "cache"), all)

	if err := appA.CleanCache("all"); err != nil {
		t.Fatalf("CleanCache(\"all\"): %v", err)
	}
	if err := appB.CleanCacheCategory("all"); err != nil {
		t.Fatalf("CleanCacheCategory(\"all\"): %v", err)
	}

	gotA := cacheFileNames(t, filepath.Join(tmpA, "cache"))
	gotB := cacheFileNames(t, filepath.Join(tmpB, "cache"))
	if len(gotA) != len(gotB) {
		t.Fatalf("CleanCache(\"all\") 与 CleanCacheCategory(\"all\") 结果不一致：%v vs %v", gotA, gotB)
	}
	for i := range gotA {
		if gotA[i] != gotB[i] {
			t.Fatalf("CleanCache(\"all\") 与 CleanCacheCategory(\"all\") 结果不一致：%v vs %v", gotA, gotB)
		}
	}
}

// TestCleanCache_SpecificCategoriesStillWork：修 F-06 不得破坏既有分类语义
// （archives / shaFiles / partFiles 各自只删本类），且未知分类仍报错。
func TestCleanCache_SpecificCategoriesStillWork(t *testing.T) {
	app, tmp := retentionApp(t)
	cacheDir := filepath.Join(tmp, "cache")
	writeF06Files(t, cacheDir, f06MetadataFiles)
	writeF06Files(t, cacheDir, f06CacheFiles)

	if err := app.CleanCache("bogus"); err == nil {
		t.Error("未知分类必须报错")
	}

	if err := app.CleanCache("archives"); err != nil {
		t.Fatalf("CleanCache(\"archives\"): %v", err)
	}
	for _, name := range []string{"go-1.26.5.zip", "llvm-22.1.8.tar.xz"} {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); !os.IsNotExist(err) {
			t.Errorf("archives 分类未删除 %s", name)
		}
	}
	// .part/.sha256 不属于 archives，必须还在。
	for _, name := range []string{"java-17.zip.sha256", "node-20.tgz.part"} {
		if _, err := os.Stat(filepath.Join(cacheDir, name)); err != nil {
			t.Errorf("archives 分类误删了非归档文件 %s", name)
		}
	}
}
