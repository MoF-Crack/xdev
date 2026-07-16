package tui

import (
	"os"
	"runtime"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// colorLevel tracks the detected color support level.
type colorLevel int

const (
	colorNone colorLevel = iota
	colorBasic
	color256
	colorTrueColor
)

var (
	detectedLevel colorLevel
	detectOnce    sync.Once
)

// detectColorSupport checks if the terminal supports true color.
// Returns the detected color level.
func detectColorSupport() colorLevel {
	detectOnce.Do(func() {
		// 1. Check NO_COLOR environment variable
		if os.Getenv("NO_COLOR") != "" {
			detectedLevel = colorNone
			return
		}
		// 2. Check COLORTERM
		ct := os.Getenv("COLORTERM")
		if ct == "truecolor" || ct == "24bit" {
			detectedLevel = colorTrueColor
			return
		}
		// 3. Windows Terminal supports TrueColor
		if runtime.GOOS == "windows" {
			if os.Getenv("WT_SESSION") != "" {
				detectedLevel = colorTrueColor
				return
			}
		}
		// 4. Check TERM for known truecolor terminals
		term := os.Getenv("TERM")
		if term == "xterm-kitty" || term == "xterm-ghostty" {
			detectedLevel = colorTrueColor
			return
		}
		// 5. Check TERM for 256-color support
		if term == "xterm-256color" || term == "screen-256color" {
			detectedLevel = color256
			return
		}
		// 6. Default: assume basic 16-color support
		detectedLevel = colorBasic
	})
	return detectedLevel
}

// Color palette — initialized with true-color values, degraded
// at init time based on terminal capability.
var (
	ColorPrimary    lipgloss.Color
	ColorSecondary  lipgloss.Color
	ColorWarning    lipgloss.Color
	ColorError      lipgloss.Color
	ColorMuted      lipgloss.Color
	ColorBgDark     lipgloss.Color
	ColorBgPanel    lipgloss.Color
	ColorBgSelected lipgloss.Color
	ColorText       lipgloss.Color
	ColorTextDim    lipgloss.Color
	ColorHighlight  lipgloss.Color
)

// ANSI color constants for degradation
const (
	ansiBlack   = "0"
	ansiRed     = "1"
	ansiGreen   = "2"
	ansiYellow  = "3"
	ansiBlue    = "4"
	ansiMagenta = "5"
	ansiCyan    = "6"
	ansiWhite   = "7"
	ansiBrightBlack   = "8"
	ansiBrightRed     = "9"
	ansiBrightGreen   = "10"
	ansiBrightYellow  = "11"
	ansiBrightBlue    = "12"
	ansiBrightMagenta = "13"
	ansiBrightCyan    = "14"
	ansiBrightWhite   = "15"
)

func init() {
	level := detectColorSupport()
	switch level {
	case colorTrueColor:
		ColorPrimary = lipgloss.Color("#7D56F4")
		ColorSecondary = lipgloss.Color("#1DB954")
		ColorWarning = lipgloss.Color("#FFD700")
		ColorError = lipgloss.Color("#FF4757")
		ColorMuted = lipgloss.Color("#626262")
		ColorBgDark = lipgloss.Color("#1A1A2E")
		ColorBgPanel = lipgloss.Color("#16213E")
		ColorBgSelected = lipgloss.Color("#0F3460")
		ColorText = lipgloss.Color("#E0E0E0")
		ColorTextDim = lipgloss.Color("#888888")
		ColorHighlight = lipgloss.Color("#FFFFFF")
	case color256:
		ColorPrimary = lipgloss.Color("98")   // purple
		ColorSecondary = lipgloss.Color("40")  // green
		ColorWarning = lipgloss.Color("220")   // yellow
		ColorError = lipgloss.Color("196")     // red
		ColorMuted = lipgloss.Color("241")     // gray
		ColorBgDark = lipgloss.Color("235")    // dark blue-gray
		ColorBgPanel = lipgloss.Color("236")   // dark blue
		ColorBgSelected = lipgloss.Color("24") // dark blue
		ColorText = lipgloss.Color("254")      // light gray
		ColorTextDim = lipgloss.Color("245")   // dim gray
		ColorHighlight = lipgloss.Color("15")  // white
	case colorBasic:
		ColorPrimary = lipgloss.Color(ansiMagenta)
		ColorSecondary = lipgloss.Color(ansiGreen)
		ColorWarning = lipgloss.Color(ansiYellow)
		ColorError = lipgloss.Color(ansiRed)
		ColorMuted = lipgloss.Color(ansiBrightBlack)
		ColorBgDark = lipgloss.Color(ansiBlack)
		ColorBgPanel = lipgloss.Color(ansiBlack)
		ColorBgSelected = lipgloss.Color(ansiBlue)
		ColorText = lipgloss.Color(ansiWhite)
		ColorTextDim = lipgloss.Color(ansiBrightBlack)
		ColorHighlight = lipgloss.Color(ansiBrightWhite)
	default: // colorNone
		ColorPrimary = lipgloss.Color("")
		ColorSecondary = lipgloss.Color("")
		ColorWarning = lipgloss.Color("")
		ColorError = lipgloss.Color("")
		ColorMuted = lipgloss.Color("")
		ColorBgDark = lipgloss.Color("")
		ColorBgPanel = lipgloss.Color("")
		ColorBgSelected = lipgloss.Color("")
		ColorText = lipgloss.Color("")
		ColorTextDim = lipgloss.Color("")
		ColorHighlight = lipgloss.Color("")
	}
}

// Component styles
var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1)

	StylePanelFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorHighlight).
				Padding(0, 1)

	StyleSelected = lipgloss.NewStyle().
			Background(ColorBgSelected).
			Foreground(ColorHighlight).
			Bold(true)

	StyleActive = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	StyleInstalled = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	StyleNotInstalled = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	StyleStatusBar = lipgloss.NewStyle().
			Background(ColorBgDark).
			Foreground(ColorText).
			Padding(0, 1)

	StyleHelpKey = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	StyleHelpDesc = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	StyleDim = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	StyleBold = lipgloss.NewStyle().
			Bold(true)

	StyleVersionActive = lipgloss.NewStyle().
				Foreground(ColorSecondary).
				Bold(true)

	StyleVersionInstalled = lipgloss.NewStyle().
				Foreground(ColorText)

	StyleVersionRemote = lipgloss.NewStyle().
				Foreground(ColorTextDim)
)

// StatusBarHeight is the height of the bottom status bar.
const StatusBarHeight = 2

// TitleBarHeight is the height of the top title bar.
const TitleBarHeight = 1

// MinWidth is the minimum terminal width for the TUI.
const MinWidth = 80

// MinHeight is the minimum terminal height for the TUI.
const MinHeight = 24
