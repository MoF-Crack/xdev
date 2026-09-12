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
	Name             string     `json:"name"`
	Value            string     `json:"value"`
	Source           string     `json:"source"`
	Level            string     `json:"level"`
	ToolName         string     `json:"toolName"`
	IsValid          bool       `json:"isValid"`
	InvalidReason    string     `json:"invalidReason"`
	ValidationStatus string     `json:"validationStatus"`
	PathEntries      []PathEntry `json:"pathEntries,omitempty"`
}

// PathEntry represents a single semicolon-delimited path within an env var
// value, with its individual validity status. Used by the frontend env var
// editor to show per-path validity without parsing the human-readable
// invalidReason string.
type PathEntry struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

func validationStatus(value string, valid bool, reason string) string {
	if !strings.Contains(value, ":\\") && !strings.Contains(value, ":/") && !strings.HasPrefix(value, "\\") && !strings.HasPrefix(value, "/") && !strings.Contains(value, "/") {
		return "not-applicable"
	}
	if valid {
		return "valid"
	}
	if strings.Contains(value, ";") && strings.Contains(reason, "/") {
		return "partial"
	}
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
					PathEntries: buildPathEntries(val),
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
					PathEntries: buildPathEntries(val),
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
					PathEntries: buildPathEntries(val),
				})
			}
		}
	}

	return result
}

// buildPathEntries splits a semicolon-delimited env var value into individual
// path entries with per-entry existence checks. Non-path values (no path-like
// characters) return nil so the frontend knows not to show a path list.
func buildPathEntries(value string) []PathEntry {
	if value == "" {
		return nil
	}
	expanded := os.ExpandEnv(value)
	looksLikePath := strings.Contains(expanded, ":\\") || strings.Contains(expanded, ":/") ||
		strings.HasPrefix(expanded, "\\") || strings.HasPrefix(expanded, "/") ||
		strings.Contains(expanded, "/")
	if !looksLikePath {
		return nil
	}
	// Split on the RAW (unexpanded) value so the frontend sees the original
	// string the user typed, not the expanded one. Existence is checked on
	// the expanded form.
	parts := strings.Split(value, ";")
	var entries []PathEntry
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		expandedP := os.ExpandEnv(p)
		_, err := os.Stat(expandedP)
		entries = append(entries, PathEntry{Path: p, Exists: err == nil})
	}
	return entries
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
			PathEntries: buildPathEntries(pair.Value),
		})
	}
	return result
}

func (a *App) DeleteEnvVarEntry(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot delete protected system env var: %s", name)
	}
	switch level {
	case "user", "machine":
		return env.DeleteEnvVar(name, level)
	default:
		return fmt.Errorf("unsupported level: %q, only user or machine", level)
	}
}

func (a *App) CleanEnvVarPath(name, level string) error {
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
	}
	logInfo("[CleanEnvVarPath] cleaning env var: %s", name)

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

// CleanAllInvalidEnvVars iterates all env vars at the given level and cleans
// every one that is invalid or partially valid (contains non-existent paths).
// Returns a summary of what was cleaned. Protected system vars are skipped.
type CleanResult struct {
	Name   string `json:"name"`
	Action string `json:"action"` // "cleaned" | "deleted" | "skipped"
	Detail string `json:"detail"`
}

func (a *App) CleanAllInvalidEnvVars(level string) []CleanResult {
	logInfo("[CleanAllInvalidEnvVars] level=%s", level)
	vars := a.GetAllEnvVars(level)
	if len(vars) == 0 {
		return nil
	}
	var results []CleanResult
	for _, v := range vars {
		if util.IsProtectedSystemEnvVar(v.Name) && !strings.EqualFold(v.Name, "path") {
			results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: "protected"})
			continue
		}
		if v.IsValid {
			continue // valid �?nothing to do
		}
		// invalid or partial �?try cleaning
		err := a.CleanEnvVarPath(v.Name, level)
		if err != nil {
			if strings.Contains(err.Error(), "admin") || strings.Contains(err.Error(), "ACCESS_DENIED") || strings.Contains(err.Error(), "error 5") {
				results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: "admin required"})
			} else {
				results = append(results, CleanResult{Name: v.Name, Action: "skipped", Detail: err.Error()})
			}
			continue
		}
		// Check if var was deleted (all paths invalid) or just cleaned (some paths removed)
		remaining, readErr := env.GetEnvVar(v.Name, level)
		if readErr != nil || remaining == "" {
			results = append(results, CleanResult{Name: v.Name, Action: "deleted", Detail: "all paths were invalid"})
		} else {
			results = append(results, CleanResult{Name: v.Name, Action: "cleaned", Detail: "removed invalid paths"})
		}
	}
	return results
}

func (a *App) SetEnvVar(toolName, name, value, level string) error {
	if err := util.ValidateEnvVarName(name); err != nil && !strings.EqualFold(name, "path") {
		return err
	}
	if util.IsProtectedSystemEnvVar(name) && !strings.EqualFold(name, "path") {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
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
	if util.IsProtectedSystemEnvVar(name) {
		return fmt.Errorf("cannot modify protected system env var: %s", name)
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

func (a *App) SwitchToLocalVersion(toolName, localPath string) error {
	logInfo("[SwitchToLocalVersion] switching %s to local version", toolName)

	// Ticket 06 (audit-fixes / P0-08): validate everything BEFORE touching
	// the filesystem. Previously toolName was used to build
	// current/<toolName> and the link was removed/re-created before the
	// plugin lookup ran — a traversal name (e.g. "..\\current\\node")
	// attacked the real link, and an unregistered tool name still left the
	// link dangling after the late failure.
	if err := util.ValidateShimName(toolName); err != nil {
		return fmt.Errorf("invalid tool name %q: %w", toolName, err)
	}
	// Reject unknown tools up front: the plugin also drives the shim
	// refresh below, so a "switch" for an unknown tool can never be valid.
	p, err := a.mustGetPlugin(toolName)
	if err != nil {
		return err
	}

	cfg := a.getCfg()

	path := strings.TrimSpace(localPath)
	if path == "" {
		_, cached, _ := a.getLocalCached(toolName)
		path = cached
	}
	if path == "" {
		return fmt.Errorf("no local installation found for %s", toolName)
	}
	// Security: localPath arrives from the WebView and becomes the
	// current/<tool> junction target — require an existing, real (non-
	// symlink) directory so the binding cannot point a tool at an
	// arbitrary location. The cached path was already scan-verified, but
	// an explicit path argument must be re-checked here.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("local path must be absolute: %q", path)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return fmt.Errorf("local path is not an existing directory: %q", path)
	}

	currentLink := a.currentLink(toolName)
	// Ticket 06: a failed link removal must fail the switch — the old code
	// only logged, leaving CreateJunction to run against a live link.
	if err := os.Remove(currentLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing current link %s: %w", currentLink, err)
	}
	// Ticket 06: a failed junction creation must fail the switch — the old
	// code logged "falling back" (there was no fallback) and returned nil,
	// reporting success with the tool's link now missing entirely.
	if err := util.CreateJunction(currentLink, path); err != nil {
		return fmt.Errorf("creating junction %s -> %s: %w", currentLink, path, err)
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
	logInfo("[SwitchToLocalVersion] %s switched to local version at %s", toolName, path)
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
