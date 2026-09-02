// messages.go defines the Bubble Tea message types used for async
// communication between the install goroutine and the TUI update loop.
//
// # Stale Message Detection
//
// Both InstallResultMsg and InstallProgressMsg carry an ID field that
// matches InstallState.ID. The Update handler checks this ID and discards
// messages that don't match the current install. This prevents race
// conditions when the user rapidly switches between tools while an
// install is in progress.
//
// # Message Flow
//
//  1. User presses Enter → startInstall() creates InstallState with unique ID
//  2. doInstall() runs in a goroutine, sends InstallProgressMsg periodically
//  3. Update() receives InstallProgressMsg, checks ID, updates progress bar
//  4. doInstall() completes, sends InstallResultMsg
//  5. Update() receives InstallResultMsg, checks ID, shows success/error
//  6. RefreshMsg is sent to reload installed versions for the current tool
package tui

// InstallResultMsg is sent when an install operation completes.
type InstallResultMsg struct {
	ID      int64 // matches InstallState.ID to discard stale results
	Tool    string
	Version string
	Path    string
	Err     error
}

// InstallProgressMsg is sent periodically during installation.
type InstallProgressMsg struct {
	ID      int64 // matches InstallState.ID to discard stale progress
	Tool    string
	Version string
	Phase   InstallPhase
	Percent float64
	Speed   string
	ETA     string
}

// InstallPhase represents the current phase of installation.
type InstallPhase int

const (
	PhaseDownloading InstallPhase = iota
	PhaseExtracting
	PhaseInstalling
	PhaseActivating
)

func (p InstallPhase) String() string {
	switch p {
	case PhaseDownloading:
		return "Downloading"
	case PhaseExtracting:
		return "Extracting"
	case PhaseInstalling:
		return "Installing"
	case PhaseActivating:
		return "Activating"
	default:
		return "Unknown"
	}
}

// RemoteVersionsMsg is sent when remote version listing completes.
type RemoteVersionsMsg struct {
	Tool     string
	Versions []string
	Err      error
}

// RefreshMsg is sent to trigger a data refresh after install/uninstall.
type RefreshMsg struct{}

// WindowTooSmallMsg is sent when the terminal is too small.
type WindowTooSmallMsg struct {
	Width  int
	Height int
}
