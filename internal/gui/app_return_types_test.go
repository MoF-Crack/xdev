package gui

import (
	"encoding/json"
	"testing"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/plugin"
)

// ──────────────────────────────────────────────────────
// 回归测试：验证 9 个强类型返回方法的 JSON 序列化格式
// 与之前 map[string]interface{} 版本保持兼容
// ──────────────────────────────────────────────────────

func newTestApp() *App {
	cfg := &config.Config{
		Core: config.CoreConfig{
			XMDevDir:    "E:\\XDev",
			DownloadDir: "E:\\XDev\\download",
			InstallDir:  "E:\\XDev\\install",
			ShimDir:     "E:\\XDev\\shims",
			CacheDir:    "E:\\XDev\\cache",
			EnvDir:      "E:\\XDev\\env",
			ManifestDir: "E:\\XDev\\manifests",
		},
		Path: config.PathConfig{
			Mode: "shim",
		},
		Proxy: config.ProxyConfig{
			HTTP:    "http://127.0.0.1:7890",
			HTTPS:   "http://127.0.0.1:7890",
			NoProxy: "localhost,127.0.0.1",
			Enabled: true,
		},
		Mirror: map[string]string{
			"go": "https://goproxy.cn",
		},
	}
	cat := &manifest.Catalog{}
	reg := plugin.NewRegistry()
	return NewApp(cfg, cat, reg)
}

// assertJSONKeys 验证 JSON 反序列化后的 map 包含指定的键
func assertJSONKeys(t *testing.T, data []byte, expectedKeys []string) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("JSON 反序列化失败: %v\n原始数据: %s", err, string(data))
	}
	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("缺少键 %q, 实际键: %v", key, keysOf(m))
		}
	}
	// 检查没有多余的键（omitempty 的键在零值时不出现在 JSON 中）
	expectedSet := make(map[string]bool)
	for _, k := range expectedKeys {
		expectedSet[k] = true
	}
	for k := range m {
		if !expectedSet[k] {
			t.Errorf("存在未预期的键: %q", k)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ──────────────────────────────────────────────────────
// 1. GetDebugInfo
// ──────────────────────────────────────────────────────

func TestGetDebugInfo_JSONFormat(t *testing.T) {
	app := newTestApp()
	result := app.GetDebugInfo()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{
		"xmdevDir", "manifestDir", "cacheDir",
		"registryCount", "catalogCount",
		"registryNames", "catalogNames",
	})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["xmdevDir"].(string); !ok {
		t.Errorf("xmdevDir 应为 string, 实际: %T", m["xmdevDir"])
	}
	if _, ok := m["registryCount"].(float64); !ok {
		t.Errorf("registryCount 应为 number, 实际: %T", m["registryCount"])
	}
	if _, ok := m["registryNames"].([]interface{}); !ok {
		t.Errorf("registryNames 应为 array, 实际: %T", m["registryNames"])
	}
}

// ──────────────────────────────────────────────────────
// 2. GetDashboardStats
// ──────────────────────────────────────────────────────

func TestGetDashboardStats_JSONFormat(t *testing.T) {
	// GetDashboardStats is now async (returns cached/zero value immediately),
	// so we test the JSON format by constructing a DashboardStats directly.
	result := DashboardStats{
		TotalTools:     35,
		InstalledCount: 10,
		InstallSize:    1024000,
		CacheSize:      512000,
		ShimCount:      10,
		XMDevDir:       "E:\\XDev",
		InstallDir:     "E:\\XDev\\install",
		ShimDir:        "E:\\XDev\\shims",
		CacheDir:       "E:\\XDev\\cache",
		DownloadDir:    "E:\\XDev\\download",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{
		"totalTools", "installedCount", "installSize", "cacheSize",
		"shimCount", "xmdevDir", "installDir", "shimDir",
		"cacheDir", "downloadDir", "installedTools",
	})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["totalTools"].(float64); !ok {
		t.Errorf("totalTools 应为 number, 实际: %T", m["totalTools"])
	}
	if _, ok := m["installSize"].(float64); !ok {
		t.Errorf("installSize 应为 number, 实际: %T", m["installSize"])
	}
	if _, ok := m["xmdevDir"].(string); !ok {
		t.Errorf("xmdevDir 应为 string, 实际: %T", m["xmdevDir"])
	}
}

// ──────────────────────────────────────────────────────
// 3. CheckForUpdate — 错误分支
// ──────────────────────────────────────────────────────

func TestCheckForUpdate_ErrorBranch_JSONFormat(t *testing.T) {
	result := UpdateCheckResult{
		Error:     "network error",
		HasUpdate: false,
		Current:   "1.1.9-debug",
		Latest:    "",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{
		"error", "hasUpdate", "current", "latest",
	})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["hasUpdate"].(bool); !ok {
		t.Errorf("hasUpdate 应为 bool, 实际: %T", m["hasUpdate"])
	}
	if _, ok := m["current"].(string); !ok {
		t.Errorf("current 应为 string, 实际: %T", m["current"])
	}
}

// ──────────────────────────────────────────────────────
// 3b. CheckForUpdate — 成功分支（无 error 字段）
// ──────────────────────────────────────────────────────

func TestCheckForUpdate_SuccessBranch_JSONFormat(t *testing.T) {
	result := UpdateCheckResult{
		HasUpdate: true,
		Current:   "1.1.9-debug",
		Latest:    "1.2.0",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 成功时 error 为空，omitempty 导致不出现在 JSON 中
	assertJSONKeys(t, data, []string{
		"hasUpdate", "current", "latest",
	})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["hasUpdate"].(bool); !ok {
		t.Errorf("hasUpdate 应为 bool, 实际: %T", m["hasUpdate"])
	}
}

// ──────────────────────────────────────────────────────
// 4. RunDiagnostics
// ──────────────────────────────────────────────────────

func TestRunDiagnostics_JSONFormat(t *testing.T) {
	results := []DiagnosticResult{
		{
			Check: "XMDevDir",
			Path:  "E:\\XDev",
			OK:    true,
		},
		{
			Check: "ShimDir in PATH",
			Path:  "E:\\XDev\\shims",
			OK:    false,
		},
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("期望 2 个诊断结果, 实际: %d", len(arr))
	}

	// 目录检查项：OK=true 时 error 为空，omitempty 不出现
	dirCheckKeys := []string{"check", "path", "ok"}
	for _, key := range dirCheckKeys {
		if _, ok := arr[0][key]; !ok {
			t.Errorf("目录检查缺少键 %q", key)
		}
	}

	// PATH 检查项
	pathCheckKeys := []string{"check", "path", "ok"}
	for _, key := range pathCheckKeys {
		if _, ok := arr[1][key]; !ok {
			t.Errorf("PATH 检查缺少键 %q", key)
		}
	}

	// 验证类型
	if _, ok := arr[0]["ok"].(bool); !ok {
		t.Errorf("ok 应为 bool, 实际: %T", arr[0]["ok"])
	}
	if _, ok := arr[0]["check"].(string); !ok {
		t.Errorf("check 应为 string, 实际: %T", arr[0]["check"])
	}
}

// ──────────────────────────────────────────────────────
// 4b. RunDiagnostics — 错误项包含 error 字段
// ──────────────────────────────────────────────────────

func TestRunDiagnostics_WithErrors_JSONFormat(t *testing.T) {
	results := []DiagnosticResult{
		{
			Check: "CacheDir",
			Path:  "E:\\XDev\\cache",
			OK:    false,
			Error: "directory does not exist",
		},
	}
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var arr []map[string]interface{}
	json.Unmarshal(data, &arr)

	assertJSONKeys(t, mustMarshal(t, arr[0]), []string{"check", "path", "ok", "error"})
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return data
}

// ──────────────────────────────────────────────────────
// 5. GetConfig
// ──────────────────────────────────────────────────────

func TestGetConfig_JSONFormat(t *testing.T) {
	app := newTestApp()
	result := app.GetConfig()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{
		"xmdevDir", "installDir", "shimDir", "cacheDir",
		"downloadDir", "pathMode", "proxy",
	})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	// 验证 proxy 是嵌套对象
	proxy, ok := m["proxy"].(map[string]interface{})
	if !ok {
		t.Fatalf("proxy 应为 object, 实际: %T", m["proxy"])
	}
	proxyKeys := []string{"http", "https", "noProxy", "enabled"}
	for _, key := range proxyKeys {
		if _, ok := proxy[key]; !ok {
			t.Errorf("proxy 缺少键 %q", key)
		}
	}

	// 验证 proxy 字段类型
	if _, ok := proxy["http"].(string); !ok {
		t.Errorf("proxy.http 应为 string, 实际: %T", proxy["http"])
	}
	if _, ok := proxy["enabled"].(bool); !ok {
		t.Errorf("proxy.enabled 应为 bool, 实际: %T", proxy["enabled"])
	}
}

// ──────────────────────────────────────────────────────
// 6. MigrateAllLocalTools — 三种状态
// ──────────────────────────────────────────────────────

func TestMigrateAllLocalTools_Skipped_JSONFormat(t *testing.T) {
	result := MigrationResult{
		Name:   "node",
		Status: "skipped",
		Reason: "已由 xdev 管理",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	assertJSONKeys(t, data, []string{"name", "status", "reason"})
}

func TestMigrateAllLocalTools_Failed_JSONFormat(t *testing.T) {
	result := MigrationResult{
		Name:   "python",
		Status: "failed",
		Error:  "权限不足",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	assertJSONKeys(t, data, []string{"name", "status", "error"})
}

func TestMigrateAllLocalTools_Success_JSONFormat(t *testing.T) {
	result := MigrationResult{
		Name:    "go",
		Status:  "success",
		Version: "1.22.0",
		Path:    "C:\\Go",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	assertJSONKeys(t, data, []string{"name", "status", "version", "path"})
}

// ──────────────────────────────────────────────────────
// 7. GetProxyConfig
// ──────────────────────────────────────────────────────

func TestGetProxyConfig_JSONFormat(t *testing.T) {
	app := newTestApp()
	result := app.GetProxyConfig()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{"http", "https", "noProxy", "enabled"})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["http"].(string); !ok {
		t.Errorf("http 应为 string, 实际: %T", m["http"])
	}
	if _, ok := m["enabled"].(bool); !ok {
		t.Errorf("enabled 应为 bool, 实际: %T", m["enabled"])
	}
}

// ──────────────────────────────────────────────────────
// 8. TestProxy — 成功和失败分支
// ──────────────────────────────────────────────────────

func TestTestProxy_Failure_JSONFormat(t *testing.T) {
	result := ProxyTestResult{
		Success: false,
		Message: "连接失败: dial tcp 127.0.0.1:7890: connection refused",
		Latency: 0,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{"success", "message", "latency"})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["success"].(bool); !ok {
		t.Errorf("success 应为 bool, 实际: %T", m["success"])
	}
	if _, ok := m["latency"].(float64); !ok {
		t.Errorf("latency 应为 number, 实际: %T", m["latency"])
	}
}

func TestTestProxy_Success_JSONFormat(t *testing.T) {
	result := ProxyTestResult{
		Success: true,
		Message: "连接成功 (HTTP 200, 156ms)",
		Latency: 156,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{"success", "message", "latency"})
}

// ──────────────────────────────────────────────────────
// 9. ImportProfile — 成功和失败分支
// ──────────────────────────────────────────────────────

func TestImportProfile_Failure_JSONFormat(t *testing.T) {
	result := ImportResult{
		Success:   false,
		Message:   "读取文件失败: open xdev-profile.json: no such file or directory",
		Installed: 0,
		Failed:    0,
		Errors:    []string{},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{"success", "message", "installed", "failed", "errors"})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["success"].(bool); !ok {
		t.Errorf("success 应为 bool, 实际: %T", m["success"])
	}
	if _, ok := m["errors"].([]interface{}); !ok {
		t.Errorf("errors 应为 array, 实际: %T", m["errors"])
	}
}

func TestImportProfile_Success_JSONFormat(t *testing.T) {
	result := ImportResult{
		Success:   true,
		Message:   "导入完成: 3 个工具已安装, 1 个失败",
		Installed: 3,
		Failed:    1,
		Errors:    []string{"rust@1.77.0: 下载超时"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	assertJSONKeys(t, data, []string{"success", "message", "installed", "failed", "errors"})

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	errs, ok := m["errors"].([]interface{})
	if !ok {
		t.Fatalf("errors 应为 array, 实际: %T", m["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("errors 应有 1 个元素, 实际: %d", len(errs))
	}
	if _, ok := errs[0].(string); !ok {
		t.Errorf("errors[0] 应为 string, 实际: %T", errs[0])
	}
}

// ──────────────────────────────────────────────────────
// 跨方法一致性测试
// ──────────────────────────────────────────────────────

func TestGetProxyConfig_MatchesGetConfigProxy(t *testing.T) {
	app := newTestApp()
	proxyConfig := app.GetProxyConfig()
	fullConfig := app.GetConfig()

	proxyData, _ := json.Marshal(proxyConfig)
	configProxyData, _ := json.Marshal(fullConfig.Proxy)

	var proxyMap, configProxyMap map[string]interface{}
	json.Unmarshal(proxyData, &proxyMap)
	json.Unmarshal(configProxyData, &configProxyMap)

	for key := range configProxyMap {
		if _, ok := proxyMap[key]; !ok {
			t.Errorf("GetProxyConfig 缺少 GetConfig.proxy 中的键 %q", key)
		}
	}
	for key := range proxyMap {
		if _, ok := configProxyMap[key]; !ok {
			t.Errorf("GetConfig.proxy 缺少 GetProxyConfig 中的键 %q", key)
		}
	}
}

func TestGetDashboardStats_ContainsGetConfigDirs(t *testing.T) {
	// DashboardStats is now async, so we test field consistency
	// by constructing both types with the same config values.
	cfg := config.Config{
		Core: config.CoreConfig{
			XMDevDir:    "E:\\XDev",
			DownloadDir: "E:\\XDev\\download",
			InstallDir:  "E:\\XDev\\install",
			ShimDir:     "E:\\XDev\\shims",
			CacheDir:    "E:\\XDev\\cache",
		},
	}

	stats := DashboardStats{
		XMDevDir:    cfg.Core.XMDevDir,
		InstallDir:  cfg.Core.InstallDir,
		ShimDir:     cfg.Core.ShimDir,
		CacheDir:    cfg.Core.CacheDir,
		DownloadDir: cfg.Core.DownloadDir,
	}
	configInfo := ConfigInfo{
		XMDevDir:    cfg.Core.XMDevDir,
		InstallDir:  cfg.Core.InstallDir,
		ShimDir:     cfg.Core.ShimDir,
		CacheDir:    cfg.Core.CacheDir,
		DownloadDir: cfg.Core.DownloadDir,
	}

	statsMap := mustMarshal(t, stats)
	cfgMap := mustMarshal(t, configInfo)

	var statsObj, cfgObj map[string]interface{}
	json.Unmarshal(statsMap, &statsObj)
	json.Unmarshal(cfgMap, &cfgObj)

	dirKeys := []string{"xmdevDir", "installDir", "shimDir", "cacheDir", "downloadDir"}
	for _, key := range dirKeys {
		statsVal, ok1 := statsObj[key]
		cfgVal, ok2 := cfgObj[key]
		if !ok1 || !ok2 {
			t.Errorf("键 %q 在某个返回值中缺失", key)
			continue
		}
		if statsVal != cfgVal {
			t.Errorf("键 %q 不一致: stats=%v, config=%v", key, statsVal, cfgVal)
		}
	}
}

// ──────────────────────────────────────────────────────
// omitempty 行为验证
// ──────────────────────────────────────────────────────

func TestDiagnosticResult_OmitEmpty_Error(t *testing.T) {
	// OK=true 时 Error 为空，应不出现在 JSON 中
	dr := DiagnosticResult{Check: "test", Path: "/tmp", OK: true}
	data, _ := json.Marshal(dr)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if _, exists := m["error"]; exists {
		t.Error("DiagnosticResult.Error 为空时应被 omitempty 忽略")
	}

	// OK=false 时 Error 有值，应出现在 JSON 中
	dr2 := DiagnosticResult{Check: "test", Path: "/tmp", OK: false, Error: "not found"}
	data2, _ := json.Marshal(dr2)
	var m2 map[string]interface{}
	json.Unmarshal(data2, &m2)
	if _, exists := m2["error"]; !exists {
		t.Error("DiagnosticResult.Error 有值时应出现在 JSON 中")
	}
}

func TestUpdateCheckResult_OmitEmpty_Error(t *testing.T) {
	// 成功时 Error 为空
	ucr := UpdateCheckResult{HasUpdate: true, Current: "1.0", Latest: "2.0"}
	data, _ := json.Marshal(ucr)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if _, exists := m["error"]; exists {
		t.Error("UpdateCheckResult.Error 为空时应被 omitempty 忽略")
	}

	// 失败时 Error 有值
	ucr2 := UpdateCheckResult{HasUpdate: false, Current: "1.0", Latest: "", Error: "timeout"}
	data2, _ := json.Marshal(ucr2)
	var m2 map[string]interface{}
	json.Unmarshal(data2, &m2)
	if _, exists := m2["error"]; !exists {
		t.Error("UpdateCheckResult.Error 有值时应出现在 JSON 中")
	}
}

func TestMigrationResult_OmitEmpty_Fields(t *testing.T) {
	// skipped 状态：只有 name, status, reason
	mr := MigrationResult{Name: "node", Status: "skipped", Reason: "已管理"}
	data, _ := json.Marshal(mr)
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if _, exists := m["version"]; exists {
		t.Error("MigrationResult.Version 为空时应被 omitempty 忽略")
	}
	if _, exists := m["path"]; exists {
		t.Error("MigrationResult.Path 为空时应被 omitempty 忽略")
	}
	if _, exists := m["error"]; exists {
		t.Error("MigrationResult.Error 为空时应被 omitempty 忽略")
	}
	if _, exists := m["reason"]; !exists {
		t.Error("MigrationResult.Reason 有值时应出现在 JSON 中")
	}
}
