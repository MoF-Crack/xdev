// app_custom_tools.go implements the custom tool management methods for the GUI.
//
// Provides:
//   - PreviewCustomToolVersions: discover versions without writing a manifest
//   - AddCustomTool: discover versions, generate manifest, save config
//   - RemoveCustomTool: delete on-disk manifest and config entry
//   - ListCustomTools: return all registered custom tools
package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xmdemt/xdev/internal/config"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/remote"
	"github.com/xmdemt/xdev/internal/util"
)

// CustomToolResult is the return type for AddCustomTool.
type CustomToolResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Versions []string `json:"versions,omitempty"`
}

// CustomToolInfo is the return type for ListCustomTools.
type CustomToolInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	SourceURL   string `json:"sourceUrl"`
	SourceType  string `json:"sourceType"`
}

// PreviewCustomToolVersions discovers available versions from a remote
// source without writing any manifest.  Returns the version list or nil
// on error.
func (a *App) PreviewCustomToolVersions(sourceURL, sourceType, pattern string) []string {
	if sourceURL == "" {
		return nil
	}
	switch sourceType {
	case "html", "json", "npm":
		if err := util.IsSafeExternalURL(sourceURL); err != nil {
			logWarn("[PreviewCustomToolVersions] unsafe URL %q: %v", sourceURL, err)
			return nil
		}
	case "github":
		if strings.Contains(sourceURL, "://") || !strings.Contains(sourceURL, "/") {
			logWarn("[PreviewCustomToolVersions] github source must use owner/repo format, got %q", sourceURL)
			return nil
		}
	}
	if pattern == "" && sourceType == "github" {
		pattern = `v?(\d+\.\d+\.\d+)$`
	}

	rs := manifest.RemoteSource{
		Type:    sourceType,
		URL:     sourceURL,
		Pattern: pattern,
	}
	versions, err := remote.DiscoverVersions(context.Background(), rs)
	if err != nil {
		logWarn("[PreviewCustomToolVersions] discover failed: %v", err)
		return nil
	}
	return versions
}

// AddCustomTool discovers versions for a custom tool, generates its
// manifest YAML, writes it to disk, saves the config entry, and reloads
// the catalog.  Returns a CustomToolResult with the discovered versions
// on success, or error details on failure.
func (a *App) AddCustomTool(name, displayName, sourceURL, sourceType, pattern, shim, envVar string) *CustomToolResult {
	// --- Validate name ---
	matched, err := regexp.MatchString(`^[a-z0-9-]+$`, name)
	if err != nil || !matched {
		return &CustomToolResult{
			Success: false,
			Message: "tool name must match ^[a-z0-9-]+$ (lowercase letters, digits, hyphens only)",
		}
	}

	// --- Validate shim name (F-08 / D11): 空则默认用工具名 ---
	if shim != "" {
		if err := util.ValidateShimName(shim); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("invalid shim name: %v", err),
			}
		}
	}

	// --- Validate envVar name (F-09 / D12): 空则默认 <NAME>_HOME ---
	if envVar != "" {
		if err := util.ValidateEnvVarName(envVar); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("invalid env var: %v", err),
			}
		}
	}

	// --- Validate sourceURL ---
	if sourceURL == "" {
		return &CustomToolResult{Success: false, Message: "source URL is required"}
	}
	switch sourceType {
	case "github":
		if strings.Contains(sourceURL, "://") || !strings.Contains(sourceURL, "/") {
			return &CustomToolResult{
				Success: false,
				Message: "GitHub source must use owner/repo format (e.g. golang/go)",
			}
		}
	case "html", "json", "npm":
		// Validate via the same safety check used by the remote package
		if err := util.IsSafeExternalURL(sourceURL); err != nil {
			return &CustomToolResult{
				Success: false,
				Message: fmt.Sprintf("%s source URL is not safe: %v", sourceType, err),
			}
		}
	}

	// --- Default pattern for GitHub ---
	if pattern == "" && sourceType == "github" {
		pattern = `v?(\d+\.\d+\.\d+)$`
	}

	// --- Discover versions ---
	rs := manifest.RemoteSource{
		Type:    sourceType,
		URL:     sourceURL,
		Pattern: pattern,
	}
	versions, err := remote.DiscoverVersions(context.Background(), rs)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("discover versions: %v", err),
		}
	}
	if len(versions) == 0 {
		return &CustomToolResult{
			Success: false,
			Message: "no versions discovered from source",
		}
	}

	// --- Convert to manifest.DiscoveredVersion ---
	discoveredVersions := make([]manifest.DiscoveredVersion, 0, len(versions))
	for _, v := range versions {
		discoveredVersions = append(discoveredVersions, manifest.DiscoveredVersion{Version: v})
	}

	// --- Generate manifest YAML ---
	yamlBytes, err := manifest.GenerateCustomManifest(name, displayName, shim, envVar, discoveredVersions)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("generate manifest: %v", err),
		}
	}

	// --- Unmarshal, set remote block, remarshal ---
	var tool manifest.Tool
	if err := yaml.Unmarshal(yamlBytes, &tool); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("parse generated manifest: %v", err),
		}
	}
	tool.Remote = manifest.RemoteSource{
		Type:    sourceType,
		URL:     sourceURL,
		Pattern: pattern,
	}
	finalYaml, err := yaml.Marshal(&tool)
	if err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("marshal manifest: %v", err),
		}
	}

	// --- Write manifest to disk ---
	cfg := a.getCfg()
	manifestPath := filepath.Join(cfg.Core.ManifestDir, name+".yaml")
	if err := os.MkdirAll(cfg.Core.ManifestDir, 0o755); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("create manifest dir: %v", err),
		}
	}
	if err := util.AtomicWriteFile(manifestPath, finalYaml, 0o644); err != nil {
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("write manifest: %v", err),
		}
	}

	// --- Update config ---
	a.cfgMu.Lock()
	a.cfg.CustomTools = append(a.cfg.CustomTools, config.CustomToolConfig{
		Name:        name,
		DisplayName: displayName,
		SourceURL:   sourceURL,
		SourceType:  sourceType,
		Pattern:     pattern,
		Shim:        shim,
		EnvVar:      envVar,
	})
	if err := a.cfg.Save(); err != nil {
		a.cfgMu.Unlock()
		return &CustomToolResult{
			Success: false,
			Message: fmt.Sprintf("save config: %v", err),
		}
	}
	a.cfgMu.Unlock()

	// --- Reload catalog + live plugin manifests ---
	if err := a.reloadCatalog(); err != nil {
		logWarn("[custom-tool] reload catalog after add: %v", err)
	}

	return &CustomToolResult{
		Success:  true,
		Message:  fmt.Sprintf("tool %q added with %d versions", name, len(versions)),
		Versions: versions,
	}
}

// RemoveCustomTool deletes the on-disk manifest for a custom tool and
// removes it from the config.  Returns an error if the tool is not
// found.
func (a *App) RemoveCustomTool(name string) error {
	// Validate name (same regex — prevent path traversal)
	matched, err := regexp.MatchString(`^[a-z0-9-]+$`, name)
	if err != nil || !matched {
		return fmt.Errorf("invalid tool name %q", name)
	}

	cfg := a.getCfg()

	// Find and remove from config
	a.cfgMu.Lock()
	idx := -1
	for i, ct := range a.cfg.CustomTools {
		if ct.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.cfgMu.Unlock()
		return fmt.Errorf("custom tool %q not found in config", name)
	}
	a.cfg.CustomTools = append(a.cfg.CustomTools[:idx], a.cfg.CustomTools[idx+1:]...)
	if err := a.cfg.Save(); err != nil {
		a.cfgMu.Unlock()
		return fmt.Errorf("save config: %w", err)
	}
	a.cfgMu.Unlock()

	// Delete manifest file (best-effort — no error if already gone)
	manifestPath := filepath.Join(cfg.Core.ManifestDir, name+".yaml")
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove manifest %s: %w", manifestPath, err)
	}

	if err := a.reloadCatalog(); err != nil {
		logWarn("[custom-tool] reload catalog after remove: %v", err)
	}

	return nil
}

// ListCustomTools returns the list of registered custom tools from config.
func (a *App) ListCustomTools() []CustomToolInfo {
	cfg := a.getCfg()
	out := make([]CustomToolInfo, 0, len(cfg.CustomTools))
	for _, ct := range cfg.CustomTools {
		out = append(out, CustomToolInfo{
			Name:        ct.Name,
			DisplayName: ct.DisplayName,
			SourceURL:   ct.SourceURL,
			SourceType:  ct.SourceType,
		})
	}
	return out
}
