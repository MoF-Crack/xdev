// messages.go defines the Bubble Tea message types used for async
// communication between the install goroutine and the TUI update loop.
//
// # Stale Message Detection
//
// Install/uninstall results and install progress messages carry an ID field
// that matches Model.currentOpID (set when the operation starts). The Update
// handler checks this ID and discards messages that don't match the current
// operation. This prevents race conditions when the user rapidly switches
// between tools, cancels an install, or uninstalls while a previous
// operation's result is still in flight.
//
// 票 40（P2-19 / E-15）：判定必须覆盖**成功与错误两条分支**，且卸载使用
// 独立的 UninstallResultMsg —— 详见 app.go 的 Update。
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
	ID      int64 // matches Model.currentOpID to discard stale results
	Tool    string
	Version string
	Path    string
	Err     error
}

// UninstallResultMsg is sent when an uninstall operation completes.
//
// 票 40（P2-19 / E-15）：卸载结果原先复用 InstallResultMsg（且
// installState==nil 时 ID 取 0）——两个操作空间共用一种消息类型，任何一侧
// 的判定放宽都会让另一侧的迟到消息被当成自己的结果。拆成独立类型后，
// 「install 的结果」与「uninstall 的结果」在类型层面就不再可能撞号；两者
// 仍各自带 ID，与 Model.currentOpID 比对。
type UninstallResultMsg struct {
	ID      int64 // matches Model.currentOpID to discard stale results
	Tool    string
	Version string
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
