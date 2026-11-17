package cli

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// tuiLogFile keeps the redirect target open for the process lifetime; closing
// it would send subsequent log writes to a closed file.
var tuiLogFile *os.File

// tuiLogPath is where TUI/GUI startup diagnostics (update check + startup
// sniff) are written instead of the terminal.
func tuiLogPath(cacheDir string) string {
	return filepath.Join(cacheDir, "logs", "tui.log")
}

// redirectInteractiveLogs detaches log output from stderr before the Bubble Tea
// / Wails UI starts. Startup sniff is intentionally background work, but its
// per-tool progress logs must not paint over the TUI.
func redirectInteractiveLogs() {
	cfg := Cfg()
	if cfg == nil {
		log.SetOutput(io.Discard)
		return
	}
	if err := redirectLogsTo(tuiLogPath(cfg.Core.CacheDir)); err != nil {
		// Never let logging setup break the UI; drop diagnostics instead.
		log.SetOutput(io.Discard)
	}
}

// redirectLogsTo points the standard logger at path (created 0600).
func redirectLogsTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	tuiLogFile = f
	log.SetOutput(f)
	return nil
}
