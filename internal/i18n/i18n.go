package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	currentLang = "en"
	translations = map[string]map[string]string{
		"en": enTranslations,
		"zh": zhTranslations,
	}
	mu sync.RWMutex
)

// enTranslations is the embedded English translation map (fallback).
var enTranslations = map[string]string{
	// Common status
	"installed":     "installed",
	"activated":     "activated",
	"uninstalled":   "uninstalled",
	"error":         "error",
	"success":       "success",
	"not_installed": "not installed",
	"active":        "active",

	// CLI messages
	"cli.installed_to":      "installed %s@%s to %s",
	"cli.activated":         "activated %s@%s (current -> %s)",
	"cli.removed":           "removed %s@%s",
	"cli.now_active":        "%s@%s is now active",
	"cli.no_active_version": "no active version for %s",
	"cli.no_versions":       "(no %s versions installed)",
	"cli.no_tools_activated": "(no tools activated)",
	"cli.nothing_installed":  "(nothing installed yet)",
	"cli.unsupported_tool":  "unsupported tool %q",
	"cli.no_default_version": "no default version for %s; specify @<version>",
	"cli.uninstall_requires_version": "uninstall requires an explicit version (e.g. %s@3.12.0)",
	"cli.use_requires_version":       "use requires a version, e.g. %s@3.12.0",
	"cli.not_installed_run":          "%s@%s is not installed (run `xdev install %s@%s`)",
	"cli.tip_new_terminal":           "Tip: open a new terminal or `source ~/.zshrc` / `~/.bashrc` to use the new shims.",
	"cli.no_tool_matches":            "no tool matches %q",
	"cli.warning_load_manifests":     "warning: failed to load manifests from %s: %v",
}

// zhTranslations is the embedded Chinese translation map.
var zhTranslations = map[string]string{
	// Common status
	"installed":     "已安装",
	"activated":     "已激活",
	"uninstalled":   "已卸载",
	"error":         "错误",
	"success":       "成功",
	"not_installed": "未安装",
	"active":        "活跃",

	// CLI messages
	"cli.installed_to":       "已安装 %s@%s 到 %s",
	"cli.activated":          "已激活 %s@%s (current -> %s)",
	"cli.removed":            "已移除 %s@%s",
	"cli.now_active":         "%s@%s 已激活",
	"cli.no_active_version":  "%s 没有活跃版本",
	"cli.no_versions":        "(未安装 %s 的任何版本)",
	"cli.no_tools_activated": "(没有已激活的工具)",
	"cli.nothing_installed":  "(尚未安装任何工具)",
	"cli.unsupported_tool":   "不支持的工具 %q",
	"cli.no_default_version": "%s 没有默认版本；请指定 @<version>",
	"cli.uninstall_requires_version": "卸载需要指定版本 (例如 %s@3.12.0)",
	"cli.use_requires_version":       "use 需要指定版本，例如 %s@3.12.0",
	"cli.not_installed_run":          "%s@%s 未安装 (运行 `xdev install %s@%s`)",
	"cli.tip_new_terminal":           "提示：打开新终端或执行 `source ~/.zshrc` / `~/.bashrc` 以使用新的 shim。",
	"cli.no_tool_matches":            "没有匹配 %q 的工具",
	"cli.warning_load_manifests":     "警告：从 %s 加载清单失败：%v",
}

// Init detects the system language and sets the current language accordingly.
func Init() {
	lang := detectLanguage()
	mu.Lock()
	currentLang = lang
	mu.Unlock()
}

// detectLanguage detects the language from LANG / LC_ALL environment variables.
func detectLanguage() string {
	for _, env := range []string{"LC_ALL", "LANG"} {
		if v := os.Getenv(env); v != "" {
			return normalizeLang(v)
		}
	}
	return "en"
}

// normalizeLang converts locale strings like "zh_CN.UTF-8" to "zh".
func normalizeLang(s string) string {
	s = strings.ToLower(s)
	// Remove encoding suffix (e.g. .UTF-8)
	if idx := strings.Index(s, "."); idx > 0 {
		s = s[:idx]
	}
	// Check for Chinese variants
	if strings.HasPrefix(s, "zh") {
		return "zh"
	}
	return "en"
}

// SetLanguage sets the current language for translation lookups.
func SetLanguage(lang string) {
	lang = normalizeLang(lang)
	mu.Lock()
	currentLang = lang
	mu.Unlock()
}

// Language returns the current language code.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// T translates a key to the current language, with optional fmt.Sprintf args.
func T(key string, args ...interface{}) string {
	mu.RLock()
	lang := currentLang
	mu.RUnlock()

	lookup := translations[lang]
	if lookup == nil {
		lookup = translations["en"]
	}
	text, ok := lookup[key]
	if !ok {
		// Fallback to English
		if lookup = translations["en"]; lookup != nil {
			text, ok = lookup[key]
		}
		if !ok {
			return key
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

// LoadLocaleFromFile loads translations from a YAML file for the given language.
func LoadLocaleFromFile(lang, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	var m map[string]string
	if err := yaml.Unmarshal(data, &m); err != nil {
		return err
	}
	mu.Lock()
	if translations[lang] == nil {
		translations[lang] = make(map[string]string)
	}
	for k, v := range m {
		translations[lang][k] = v
	}
	mu.Unlock()
	return nil
}
