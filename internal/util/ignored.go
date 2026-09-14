package util

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ignoredFileName 是 GUI 忽略清单的缓存文件名（与 internal/gui 的
// ignored_tools.json 一致；同一文件两端共享）。
const ignoredFileName = "ignored_tools.json"

// ignoredItem 是忽略清单条目的最小结构（与 gui.IgnoredToolInfo 的
// JSON 形状兼容：items[].{name,kind}）。这里只关心过滤需要的字段。
type ignoredItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// LoadIgnoredUpdateNames 读取 GUI 忽略清单（cacheDir/ignored_tools.json），
// 返回 kind=="update" 的工具名集合。
//
// 供嗅探调度在后台自动触发（OnStartup）时跳过用户已忽略更新的工具；
// 文件缺失/损坏时返回空集合（嗅探照常，绝不因忽略清单异常而失败）。
func LoadIgnoredUpdateNames(cacheDir string) map[string]bool {
	out := map[string]bool{}
	if cacheDir == "" {
		return out
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, ignoredFileName))
	if err != nil {
		return out
	}
	var store struct {
		Items []ignoredItem `json:"items"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return out
	}
	for _, it := range store.Items {
		if it.Kind == "update" && it.Name != "" {
			out[it.Name] = true
		}
	}
	return out
}
