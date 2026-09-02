// app_updates.go implements the GUI bindings for tool-discovery sniffing
// and available-update notifications (spec 011-tool-discovery-automanage).
package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/discovery"
	"github.com/xmdemt/xdev/internal/manifest"
	"github.com/xmdemt/xdev/internal/util"
)

// UpdateInfo represents one available tool update for the frontend.
type UpdateInfo struct {
	Tool           string `json:"tool"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Command        string `json:"command"`
}

// SniffConfigInfo exposes the current sniff configuration to the frontend.
type SniffConfigInfo struct {
	Strategy        string `json:"strategy"`
	Interval        string `json:"interval"`
	Concurrency     int    `json:"concurrency"`
	RateLimitPerHr  int    `json:"rateLimitPerHr"`
	AutoArchiveFill bool   `json:"autoArchiveFill"`
	LastSniffAt     string `json:"lastSniffAt"`
}

// ServiceStatusInfo exposes the sniff service status to the frontend.
type ServiceStatusInfo struct {
	Installed bool   `json:"installed"`
	Schedule  string `json:"schedule"`
	LastRun   string `json:"lastRun"`
	NextRun   string `json:"nextRun"`
	Error     string `json:"error,omitempty"`
}

// sniffScheduler builds a discovery.Scheduler from the current config.
func (a *App) sniffScheduler() *discovery.Scheduler {
	cfg := a.getCfg()
	return discovery.NewScheduler(&cfg.Sniff, cfg.Core.CacheDir, cfg.Core.ManifestDir)
}

// GetAvailableUpdates reads the sniff cache and returns tools that have a
// newer version available than the currently active xdev-managed version.
//
// An update is reported only when the sniffed latest version is strictly
// newer than the active install (semver compare). String inequality alone
// is not enough: a broken active directory name (e.g. garbage left by a
// bad extract) must not surface as a permanent "update available" badge.
func (a *App) GetAvailableUpdates() []UpdateInfo {
	scheduler := a.sniffScheduler()
	cache, err := scheduler.LoadCache()
	if err != nil || cache == nil {
		return nil
	}

	var updates []UpdateInfo
	for toolName, result := range cache.Tools {
		if len(result.Versions) == 0 || result.Error != "" || result.Unsupported {
			continue
		}
		activeVer, hasActive := a.currentVersion(toolName)
		if !hasActive || activeVer == "" {
			continue
		}
		// Skip non-version directory names (corrupt installs / bad strip
		// leftovers). They cannot be compared as semver and would otherwise
		// stay in the updates list forever.
		if !isComparableVersion(activeVer) {
			logDebug("[updates] skip %s: active dir %q is not a comparable version", toolName, activeVer)
			continue
		}
		latest := ""
		for _, v := range result.Versions {
			if v == "" || !isComparableVersion(v) {
				continue
			}
			if latest == "" || util.CompareSemver(v, latest) > 0 {
				latest = v
			}
		}
		if latest == "" {
			continue
		}
		// Only report when latest is strictly newer than active.
		// Equal versions (including equal after string normalize) are skipped.
		if util.CompareSemver(latest, activeVer) <= 0 {
			continue
		}
		updates = append(updates, UpdateInfo{
			Tool:           toolName,
			CurrentVersion: activeVer,
			LatestVersion:  latest,
			Command:        fmt.Sprintf("xdev install %s@%s", toolName, latest),
		})
	}

	// Filter out ignored updates (kind=update)
	before := len(updates)
	filtered := updates[:0]
	for _, u := range updates {
		if !a.isIgnored(u.Tool, "update", "") {
			filtered = append(filtered, u)
		}
	}
	updates = filtered
	if before != len(updates) {
		logDebug("[ignore] filtered updates: before=%d after=%d", before, len(updates))
	}
	return updates
}

// isComparableVersion reports whether s looks like a version string that
// CompareVersions / IsVersionNewer can reason about. Rejects empty strings
// and garbage directory names (e.g. "lld_is_a_generic_driver._").
func isComparableVersion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Must start with a digit or "v"+digit (semver-ish). Pure letter names
	// and underscore-heavy garbage paths are not versions.
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return false
	}
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	// At least one numeric segment; allow dots and common separators.
	for _, c := range s {
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' || c == '+' {
			continue
		}
		// Allow a single letter prerelease prefix like "1.2.3-rc1" handled
		// by CompareSemver (strips after '-'). Reject other letters mid-token
		// only if the whole string has no digit-dot structure — already
		// started with digit, so letters after '-' are fine.
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}

// SniffNow triggers a sniff for a single tool.
func (a *App) SniffNow(tool string) error {
	scheduler := a.sniffScheduler()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := scheduler.SniffNow(ctx, tool)
	if reloadErr := a.reloadCatalog(); reloadErr != nil {
		logWarn("[sniff] reload catalog after SniffNow: %v", reloadErr)
	}
	a.InvalidateToolsCache()
	return err
}

// SniffAll triggers a full sniff for all tools.
func (a *App) SniffAll() error {
	scheduler := a.sniffScheduler()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	err := scheduler.SniffAll(ctx)
	if reloadErr := a.reloadCatalog(); reloadErr != nil {
		logWarn("[sniff] reload catalog after SniffAll: %v", reloadErr)
	}
	a.InvalidateToolsCache()
	return err
}

// InstallSniffService installs the platform scheduled sniff service.
func (a *App) InstallSniffService(schedule string) error {
	installer, err := discovery.NewServiceInstaller()
	if err != nil {
		return err
	}
	return installer.Install(schedule)
}

// UninstallSniffService removes the platform scheduled sniff service.
func (a *App) UninstallSniffService() error {
	installer, err := discovery.NewServiceInstaller()
	if err != nil {
		return err
	}
	return installer.Uninstall()
}

// SniffServiceStatus returns the current sniff service status.
func (a *App) SniffServiceStatus() *ServiceStatusInfo {
	info := &ServiceStatusInfo{}
	installer, err := discovery.NewServiceInstaller()
	if err != nil {
		info.Error = err.Error()
		return info
	}
	status, err := installer.Status()
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Installed = status.Installed
	info.Schedule = status.Schedule
	if !status.LastRun.IsZero() {
		info.LastRun = status.LastRun.Format(time.RFC3339)
	}
	if !status.NextRun.IsZero() {
		info.NextRun = status.NextRun.Format(time.RFC3339)
	}
	return info
}

// SetSniffStrategy updates the configured sniff strategy.
func (a *App) SetSniffStrategy(strategy string) error {
	if strategy != "interval" && strategy != "startup" && strategy != "service" && strategy != "manual" {
		return fmt.Errorf("invalid strategy %q (must be interval, startup, service, or manual)", strategy)
	}
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.cfg.Sniff.Strategy = strategy
	return a.cfg.Save()
}

// GetSniffConfig returns the current sniff configuration.
// LastSniffAt is read from the sniff cache (not config), because the
// scheduler updates the cache but only occasionally persists to config
// (FR-008).
func (a *App) GetSniffConfig() *SniffConfigInfo {
	cfg := a.getCfg()
	info := &SniffConfigInfo{
		Strategy:        cfg.Sniff.Strategy,
		Interval:        cfg.Sniff.Interval,
		Concurrency:     cfg.Sniff.Concurrency,
		RateLimitPerHr:  cfg.Sniff.RateLimitPerHr,
		AutoArchiveFill: cfg.Sniff.AutoArchiveFill,
	}

	// Read LastSniffAt from cache — the scheduler writes it there
	// after each sniff.  Config.LastSniffAt may be stale.
	scheduler := a.sniffScheduler()
	cache, err := scheduler.LoadCache()
	if err == nil && cache != nil && !cache.LastSniffAt.IsZero() {
		info.LastSniffAt = cache.LastSniffAt.Format(time.RFC3339)
	} else {
		info.LastSniffAt = cfg.Sniff.LastSniffAt
	}

	return info
}

// GetLatestVersionForTool returns the latest discovered version for a tool,
// or the latest from the catalog if no sniff result is available.
// Uses semver comparison to find the true latest version (SC-006).
func (a *App) GetLatestVersionForTool(toolName string) string {
	cfg := a.getCfg()
	scheduler := a.sniffScheduler()
	cache, err := scheduler.LoadCache()
	if err == nil && cache != nil {
		if result, ok := cache.Tools[toolName]; ok && len(result.Versions) > 0 {
			latest := result.Versions[0]
			for _, v := range result.Versions[1:] {
				if util.CompareSemver(v, latest) > 0 {
					latest = v
				}
			}
			return latest
		}
	}
	cat, err := manifest.LoadCatalog(cfg.Core.ManifestDir)
	if err != nil {
		return ""
	}
	if t, ok := cat.Tool(toolName); ok {
		return t.Latest
	}
	return ""
}

// InstallUpdate installs a specific update using the existing installer
// progress mechanism. It is used by the updates badge/panel.
// Like InstallTool, it routes through InstallToolConfirmed(confirmed=false)
// so that sniff-discovered (unverified) versions trigger the Wails
// confirmation dialog (FR-018). The frontend "install" button should not
// be treated as prior confirmation of the unverified risk.
func (a *App) InstallUpdate(toolName, version string) error {
	return a.InstallToolConfirmed(toolName, version, false)
}

// allowedXdevSubcommands is the closed set of CLI verbs that the GUI
// may invoke via runXdevCommand. Anything else is treated as injection.
var allowedXdevSubcommands = map[string]struct{}{
	"sniff": {}, "list": {}, "use": {}, "install": {}, "uninstall": {},
	"current": {}, "env": {}, "version": {}, "doctor": {},
}

// runXdevCommand executes a raw xdev command string and returns its output.
// Used by update actions that need to invoke the CLI.
// Spec 035 F8: resolve our own executable path FIRST and only fall
// back to exec.LookPath — a same-named "xdev" earlier on PATH could
// otherwise shadow (or hijack) the CLI we invoke.
func (a *App) runXdevCommand(args string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe, err = exec.LookPath("xdev")
		if err != nil {
			return "", err
		}
	}
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty xdev command")
	}
	if _, ok := allowedXdevSubcommands[parts[0]]; !ok {
		return "", fmt.Errorf("xdev subcommand %q is not allowed", parts[0])
	}
	for _, p := range parts {
		if strings.ContainsAny(p, ";&|`$<>\n\r") {
			return "", fmt.Errorf("xdev command contains unsafe characters")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, parts...)
	util.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
