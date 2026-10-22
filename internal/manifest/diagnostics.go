package manifest

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ParseFile parses a single manifest file and reports the YAML error.
//
// 20261021 审计 P3-16：`LoadCatalog` 对损坏的单文件只写一行日志并跳过；
// 调用方（doctor）需要一份可枚举的“哪些文件坏了”的诊断，因此把解析入口
// 暴露出来。它只做解析，不做 catalog 合并/缓存。
func ParseFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var t Tool
	return yaml.Unmarshal(data, &t)
}
