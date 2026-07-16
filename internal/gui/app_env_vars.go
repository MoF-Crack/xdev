package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xmdemt/xdev/internal/env"
	"github.com/xmdemt/xdev/internal/util"
)

type EnvVarInfo struct {
	Name          string `json:"name"`
	Value         string `json:"value"`
	Source        string `json:"source"`
	Level         string `json:"level"`
	ToolName      string `json:"toolName"`
	IsValid       bool   `json:"isValid"`
	InvalidReason string `json:"invalidReason"`
	ValidationStatus string `json:"validationStatus"`
}

func validationStatus(value string, valid bool, reason string) string {
	if !strings.Contains(value, ":\\") && !strings.Contains(value, ":/") && !strings.HasPrefix(value, "\\") && !strings.HasPrefix(value, "/") && !strings.Contains(value, "/") { return "not-applicable" }
	if valid { return "valid" }
	if strings.Contains(value, ";") && strings.Contains(reason, "/") { return "partial" }
	return "invalid"
}

func (a *App) GetEnvVars() []EnvVarInfo {
	var result []EnvVarInfo

	devEnvVars := []string{
		"JAVA_HOME", "GOROOT", "GOPATH", "PYTHON_HOME", "PYTHONPATH",
		"NODE_PATH", "DOTNET_ROOT", "RUSTUP_HOME", "CARGO_HOME",
		"FLUTTER_ROOT", "DART_SDK", "GRADLE_HOME", "MAVEN_HOME", "M2_HOME",
		"ANDROID_HOME", "ANDROID_SDK_ROOT", "ANDROID_NDK_HOME",
		"CMAKE_ROOT", "LLVM_HOME", "NVM_HOME", "CONDA_PREFIX",
	}

	envVarToTool := map[string]string{
		"JAVA_HOME": "java", "GOROOT": "go", "GOPATH": "go",
		"PYTHON_HOME": "python", "PYTHONPATH": "python",
		"NODE_PATH": "node", "DOTNET_ROOT": "dotnet",
		"RUSTUP_HOME": "rust", "CARGO_HOME": "rust",
		"FLUTTER_ROOT": "flutter", "DART_SDK": "dart",
		"GRADLE_HOME": "gradle", "MAVEN_HOME": "maven", "M2_HOME": "maven",
		"ANDROID_HOME": "android-platform-tools", "ANDROID_SDK_ROOT": "android-platform-tools", "ANDROID_NDK_HOME": "android-ndk",
		"CMAKE_ROOT": "cmake", "LLVM_HOME": "llvm", "NVM_HOME": "node", "CONDA_PREFIX": "python",
	}

	if runtime.GOOS == "windows" {
		for _, name := range devEnvVars {
			val, err := env.GetEnvVar(name, "user")
			if err != nil {
				logDebug("[GetEnvVars] reading user env var %s failed: %v", name, err)
				continue
			}
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := ValidateEnvVar(name, val)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "user", Level: "user",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
				})
			}
		}
	}

	if runtime.GOOS == "windows" {
		for _, name := range devEnvVars {
			val, err := env.GetEnvVar(name, "machine")
			if err != nil {
				logDebug("[GetEnvVars] reading machine env var %s failed: %v", name, err)
				continue
			}
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := ValidateEnvVar(name, val)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "system", Level: "machine",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
				})
			}
		}
	}

	if runtime.GOOS != "windows" {
		for _, name := range devEnvVars {
			val := os.Getenv(name)
			if val != "" {
				toolName := envVarToTool[name]
				isValid, reason := ValidateEnvVar(name, val)
				result = append(result, EnvVarInfo{
					Name: name, Value: val, Source: "system", Level: "process",
					ToolName: toolName, IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(val, isValid, reason),
				})
			}
		}
	}

	return result
}

func ValidateEnvVar(name, value string) (bool, string) {
	if value == "" {
		return true, ""
	}

	expanded := os.ExpandEnv(value)

	looksLikePath := strings.Contains(expanded, ":\\") || strings.Contains(expanded, ":/") ||
		strings.HasPrefix(expanded, "\\") || strings.HasPrefix(expanded, "/") ||
		strings.Contains(expanded, "/")

	if !looksLikePath {
		return true, ""
	}

	if strings.Contains(expanded, ";") {
		parts := strings.Split(expanded, ";")
		var invalidParts []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				invalidParts = append(invalidParts, p)
			}
		}
		if len(invalidParts) > 0 {
			if len(invalidParts) == len(parts)-countEmpty(parts) {
				return false, fmt.Sprintf("all %d paths do not exist", len(invalidParts))
			}
			return false, fmt.Sprintf("%d/%d paths do not exist: %s",
				len(invalidParts), len(parts)-countEmpty(parts),
				truncatePaths(invalidParts, 80))
		}
		return true, ""
	}

	if _, err := os.Stat(expanded); err != nil {
		return false, fmt.Sprintf("path does not exist: %s", expanded)
	}
	return true, ""
}

func countEmpty(parts []string) int {
	count := 0
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			count++
		}
	}
	return count
}

func truncatePaths(paths []string, maxLen int) string {
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(p)
		if b.Len() > maxLen {
			return b.String()[:maxLen] + "..."
		}
	}
	return b.String()
}

func (a *App) GetAllEnvVars(level string) []EnvVarInfo {
	if runtime.GOOS != "windows" {
		var result []EnvVarInfo
		for _, pair := range os.Environ() {
			idx := strings.Index(pair, "=")
			if idx <= 0 {
				continue
			}
			name := pair[:idx]
			value := pair[idx+1:]
			isValid, reason := ValidateEnvVar(name, value)
			result = append(result, EnvVarInfo{
				Name: name, Value: value, Source: "system", Level: "process",
				IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(value, isValid, reason),
			})
		}
		return result
	}

	vars, err := env.EnumEnvVars(level)
	if err != nil {
		logDebug("[GetAllEnvVars] enumerating %s env vars failed: %v", level, err)
		return nil
	}

	var result []EnvVarInfo
	for _, pair := range vars {
		isValid, reason := ValidateEnvVar(pair.Name, pair.Value)
		result = append(result, EnvVarInfo{
			Name: pair.Name, Value: pair.Value,
			Source: level, Level: level,
			IsValid: isValid, InvalidReason: reason, ValidationStatus: validationStatus(pair.Value, isValid, reason),
		})
	}
	return result
}

func (a *App) DeleteEnvVarEntry(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot delete protected system env var: %s", name)
	}
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist, refusing delete", name)
	}
	switch level {
	case "user", "machine":
		return env.DeleteEnvVar(name, level)
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) CleanEnvVarPath(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	if !util.IsAllowedEnvVarName(name) {
		logInfo("[CleanEnvVarPath] cleaning non-whitelist env var: %s", name)
	}

	var value string
	var err error
	switch level {
	case "user", "machine":
		value, err = env.GetEnvVar(name, level)
		if err != nil {
			return fmt.Errorf("failed to read env var: %v", err)
		}
	default:
		return fmt.Errorf("unsupported level: %q", level)
	}

	if value == "" {
		return nil
	}

	expanded := os.ExpandEnv(value)

	if strings.Contains(expanded, ";") {
		parts := strings.Split(value, ";")
		var validParts []string
		removed := 0
		for _, p := range parts {
			pTrimmed := strings.TrimSpace(p)
			if pTrimmed == "" {
				continue
			}
			expandedP := os.ExpandEnv(pTrimmed)
			if _, err := os.Stat(expandedP); err != nil {
				removed++
				logInfo("[CleanEnvVarPath] removing invalid path: %s (expanded: %s)", pTrimmed, expandedP)
				continue
			}
			validParts = append(validParts, p)
		}

		if removed == 0 {
			return nil
		}

		newValue := strings.Join(validParts, ";")
		if newValue == "" {
			return env.DeleteEnvVar(name, level)
		}

		if err := env.SetEnvVar(name, newValue, level); err != nil {
			return fmt.Errorf("failed to update env var: %v", err)
		}
		logInfo("[CleanEnvVarPath] cleaned %d invalid paths from %s", removed, name)
		return nil
	}

	expandedSingle := os.ExpandEnv(value)
	if _, err := os.Stat(expandedSingle); err != nil {
		return env.DeleteEnvVar(name, level)
	}

	return nil
}

func (a *App) SetEnvVar(toolName, name, value, level string) error {
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist, refusing to write", name)
	}
	switch level {
	case "user", "machine":
		if err := env.SetEnvVar(name, value, level); err != nil {
			return err
		}
		a.autoAppendBinToPath(name, value, level)
		return nil
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) CheckEnvVarExists(name, level string) bool {
	switch level {
	case "user", "machine":
		val, err := env.GetEnvVar(name, level)
		if err != nil {
			return false
		}
		return val != ""
	}
	return false
}

func (a *App) autoAppendBinToPath(name, value, level string) {
	homeVarToTool := map[string]string{
		"JAVA_HOME": "java", "NODE_HOME": "node", "PYTHONHOME": "python",
		"GOROOT": "go", "RUSTUP_HOME": "rust", "CARGO_HOME": "rust",
		"FLUTTER_HOME": "flutter", "DOTNET_ROOT": "dotnet",
		"GRADLE_HOME": "gradle", "MAVEN_HOME": "maven", "M2_HOME": "maven",
		"ANDROID_HOME": "android-platform-tools", "ANDROID_SDK_ROOT": "android-platform-tools",
		"ANDROID_NDK_HOME": "android-ndk",
	}

	_, ok := homeVarToTool[name]
	if !ok {
		return
	}
}

func (a *App) DeleteEnvVar(toolName, name, level string) error {
	if !util.IsAllowedEnvVarName(name) {
		return fmt.Errorf("env var %q is not in the allowed whitelist, refusing delete", name)
	}
	switch level {
	case "user", "machine":
		return env.DeleteEnvVar(name, level)
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) GetOSEnvVars(level string) []EnvVarInfo {
	if runtime.GOOS != "windows" {
		return nil
	}

	vars, err := env.EnumEnvVars(level)
	if err != nil {
		logDebug("[GetOSEnvVars] enumerating %s env vars failed: %v", level, err)
		return nil
	}

	devEnvVarSet := map[string]bool{
		"JAVA_HOME": true, "GOROOT": true, "GOPATH": true, "PYTHON_HOME": true, "PYTHONPATH": true,
		"NODE_PATH": true, "DOTNET_ROOT": true, "RUSTUP_HOME": true, "CARGO_HOME": true,
		"FLUTTER_ROOT": true, "DART_SDK": true, "GRADLE_HOME": true, "MAVEN_HOME": true, "M2_HOME": true,
		"ANDROID_HOME": true, "ANDROID_SDK_ROOT": true, "ANDROID_NDK_HOME": true,
		"CMAKE_ROOT": true, "LLVM_HOME": true, "NVM_HOME": true, "CONDA_PREFIX": true,
		"Path": true, "PATH": true,
	}

	var result []EnvVarInfo
	for _, v := range vars {
		if devEnvVarSet[v.Name] {
			result = append(result, EnvVarInfo{
				Name: v.Name, Value: v.Value,
				Source: level, Level: level,
			})
		}
	}
	return result
}

func (a *App) SwitchToLocalVersion(toolName string) error {
	logInfo("[SwitchToLocalVersion] switching %s to local version", toolName)
	cfg := a.getCfg()

	_, localPath, _ := a.getLocalCached(toolName)
	if localPath == "" {
		return fmt.Errorf("no local installation found for %s", toolName)
	}

	currentLink := a.currentLink(toolName)
	if err := os.Remove(currentLink); err != nil && !os.IsNotExist(err) {
		logWarn("[SwitchToLocalVersion] removing current link %s: %v", currentLink, err)
	}
	if err := util.CreateJunction(currentLink, localPath); err != nil {
		logWarn("[SwitchToLocalVersion] creating junction failed: %v, falling back", err)
	}

	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}
	for _, shimName := range p.Shims() {
		cmdPath := filepath.Join(cfg.Core.ShimDir, shimName+".cmd")
		os.Remove(cmdPath)
	}

	xdevExe, _ := os.Executable()
	for _, shimName := range p.Shims() {
		shimPathExe := filepath.Join(cfg.Core.ShimDir, shimName+".exe")
		if _, err := os.Stat(shimPathExe); err != nil {
			if err := util.CopyFile(xdevExe, shimPathExe); err != nil {
				logWarn("[SwitchToLocalVersion] copying shim %s failed: %v", shimName, err)
			}
		}
	}

	a.refreshAndSaveLocalCache()
	logInfo("[SwitchToLocalVersion] %s switched to local version at %s", toolName, localPath)
	return nil
}

func (a *App) SwitchToXdevVersion(toolName, version string) error {
	logInfo("[SwitchToXdevVersion] switching %s to xdev version %s", toolName, version)

	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	cfg := a.getCfg()
	installPath := p.InstallDir(cfg.Core.InstallDir, version)

	if _, err := os.Stat(installPath); err != nil {
		return fmt.Errorf("version %s of %s is not installed", version, toolName)
	}

	for _, shimName := range p.Shims() {
		cmdPath := filepath.Join(cfg.Core.ShimDir, shimName+".cmd")
		os.Remove(cmdPath)
	}

	ins := a.newInstaller(cfg)
	if err := ins.Activate(p, installPath, version); err != nil {
		return fmt.Errorf("activation failed: %v", err)
	}

	a.refreshAndSaveLocalCache()
	logInfo("[SwitchToXdevVersion] %s switched to xdev version %s", toolName, version)
	return nil
}
