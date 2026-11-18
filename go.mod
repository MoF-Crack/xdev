module github.com/xmdemt/xdev

// 升级（安全）：Go 1.26.5 存在中间信任漏洞，最低构建工具链提升到 1.27.0。
// 本机实测工具链为 go1.27.3（GOTOOLCHAIN=auto）。
go 1.27.0

require (
	github.com/charmbracelet/bubbles v0.21.1-0.20250623103423-23b8fd6302d7
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834
	github.com/charmbracelet/x/term v0.2.2
	github.com/klauspost/compress v1.18.7
	github.com/spf13/cobra v1.8.1
	github.com/wailsapp/wails/v3 v3.1.0
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8
	github.com/yuin/gopher-lua v1.1.2
	golang.org/x/sync v0.23.0
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.4 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.14 // indirect
	github.com/clipperhouse/displaywidth v0.7.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.4.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
)
