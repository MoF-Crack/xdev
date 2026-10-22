package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xmdemt/xdev/internal/config"
)

// DriveChoice is the answer returned by promptDrive.
type DriveChoice struct {
	Path  string // absolute path the user wants the home to live at
	Force bool   // true when the user explicitly asked for C:
	Quit  bool   // true when the user asked to abort
}

// promptDrive scans all drives, shows a summary and asks the user
// to pick one.  It is only called when the run-time is Windows and
// no explicit --xmdevenv-dir / XMDEV_HOME was given.
//
// Behavioural notes:
//
//   - "ENTER" (empty line) accepts the recommendation (largest
//     non-system drive with >= MinFreeBytesDefault free).
//   - A single drive letter ("D" / "d" / "D:" / "d:") picks that
//     drive.
//   - "c" / "C" forces the home onto the user profile (C:) and
//     shows the strong warning later.
//   - "p" / "P" lets the user type a custom absolute path.
//   - "q" / "Q" aborts the init.
//
// When `in` is a non-interactive stream (e.g. CI piping from /dev/null)
// the helper returns the recommendation without prompting, so the
// rest of init can still make progress.
func promptDrive(in io.Reader, out io.Writer) (DriveChoice, error) {
	return promptDriveWith(in, out, config.ScanDrives())
}

// promptDriveWith is the testable core: callers can inject a custom
// drive list (handy in unit tests).  The interactive detection logic
// is the same as in production.
func promptDriveWith(in io.Reader, out io.Writer, drives []config.DriveInfo) (DriveChoice, error) {
	mounted := make([]config.DriveInfo, 0, len(drives))
	for _, d := range drives {
		if d.Mounted {
			mounted = append(mounted, d)
		}
	}
	// Find the recommendation: largest non-system drive that meets
	// the free-space threshold.
	var rec config.DriveInfo
	var recFree int64
	for _, d := range mounted {
		if d.Letter == "C:" {
			continue
		}
		if d.Free < config.MinFreeBytesDefault {
			continue
		}
		if d.Free > recFree {
			rec = d
			recFree = d.Free
		}
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Scanning drives...")
	fmt.Fprintln(out, "")
	if len(mounted) == 0 {
		fmt.Fprintln(out, "  (no fixed drives detected - defaulting to C:)")
		return DriveChoice{Path: defaultUserHome(), Force: true}, nil
	}
	fmt.Fprintf(out, "  %-6s  %14s  %14s  Path suggestion\n", "Drive", "Free", "Total")
	fmt.Fprintf(out, "  %-6s  %14s  %14s  %s\n", strings.Repeat("-", 6), strings.Repeat("-", 14), strings.Repeat("-", 14), strings.Repeat("-", 14))
	for _, d := range mounted {
		tag := "  "
		if d == rec {
			tag = "★ "
		}
		suggestion := d.Path + "XDev"
		fmt.Fprintf(out, "  %s%-4s  %14s  %14s  %s\n",
			tag,
			d.Letter,
			humanBytes(d.Free),
			humanBytes(d.Total),
			suggestion,
		)
	}
	fmt.Fprintln(out, "")
	if rec.Letter != "" {
		fmt.Fprintf(out, "  Recommended: %s (%s free)\n", rec.Letter, humanBytes(rec.Free))
	} else {
		fmt.Fprintln(out, "  Recommended: <none> - no drive meets the 20 GiB threshold")
	}
	fmt.Fprintln(out, "")

	// Detect non-interactive input.
	interactive := true
	if f, ok := in.(*os.File); ok {
		// Stat the file: regular files (piped) and /dev/null are
		// not interactive.  Windows consoles report as character
		// devices; we treat them as interactive.
		fi, err := f.Stat()
		if err == nil && fi.Mode()&os.ModeCharDevice == 0 {
			interactive = false
		}
	}
	if !interactive {
		fmt.Fprintln(out, "  (non-interactive stdin detected - using recommendation)")
		if rec.Letter == "" {
			return DriveChoice{Path: defaultUserHome(), Force: true}, nil
		}
		return DriveChoice{Path: suggestedPathFor(rec)}, nil
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "Select where xdev should live:")
		if rec.Letter != "" {
			fmt.Fprintf(out, "  [Enter]  use recommended (%s)\n", rec.Letter)
		}
		fmt.Fprintln(out, "  [D-Z]    choose a drive letter (e.g. D or D:)")
		fmt.Fprintln(out, "  [c]      force C:\\Users\\<you>\\.xdev  (NOT recommended)")
		fmt.Fprintln(out, "  [p]      type a custom absolute path")
		fmt.Fprintln(out, "  [q]      quit")
		fmt.Fprint(out, "> ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return DriveChoice{}, err
		}
		choice := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if choice == "" {
			if rec.Letter == "" {
				fmt.Fprintln(out, "  no recommendation available; please pick a drive manually.")
				continue
			}
			return DriveChoice{Path: suggestedPathFor(rec)}, nil
		}
		switch strings.ToLower(choice) {
		case "q", "quit", "exit":
			return DriveChoice{Quit: true}, nil
		case "c", "c:":
			return DriveChoice{Path: defaultUserHome(), Force: true}, nil
		case "p", "path", "custom":
			fmt.Fprint(out, "  enter absolute path: ")
			p, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return DriveChoice{}, err
			}
			p = strings.TrimSpace(strings.TrimRight(p, "\r\n"))
			if p == "" {
				fmt.Fprintln(out, "  empty path, please try again.")
				continue
			}
			// 票 33（P1-27 / E-08）：拒绝相对路径与 UNC 路径——home 一旦是
			// 相对路径，派生目录会随 CWD 漂移；UNC 无法承载 shim/注册表语义。
			if err := validateLocalAbsPath(p); err != nil {
				fmt.Fprintf(out, "  %v\n", err)
				continue
			}
			return DriveChoice{Path: p}, nil
		}
		// Treat as a drive letter.
		letter := strings.ToUpper(strings.TrimRight(choice, ":"))
		if len(letter) == 1 && letter[0] >= 'A' && letter[0] <= 'Z' {
			info, ok := findDrive(mounted, letter+":")
			if !ok {
				fmt.Fprintf(out, "  drive %s: is not mounted, please pick another.\n", letter+":")
				continue
			}
			if letter == "C" {
				fmt.Fprintf(out, "  ⚠ you picked the system drive C: (%s free)\n", humanBytes(info.Free))
				fmt.Fprintln(out, "    C: may not have enough space for Android SDK + runtimes.")
				fmt.Fprint(out, "    type 'yes' to confirm, anything else to pick again: ")
				confirm, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					return DriveChoice{}, err
				}
				if strings.TrimSpace(strings.TrimRight(confirm, "\r\n")) != "yes" {
					continue
				}
				return DriveChoice{Path: defaultUserHome(), Force: true}, nil
			}
			if info.Free < config.MinFreeBytesDefault {
				fmt.Fprintf(out, "  ⚠ %s: has only %s free (recommend %s+)\n",
					info.Letter, humanBytes(info.Free), humanBytes(config.MinFreeBytesDefault))
				fmt.Fprint(out, "    type 'yes' to confirm, anything else to pick again: ")
				confirm, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					return DriveChoice{}, err
				}
				if strings.TrimSpace(strings.TrimRight(confirm, "\r\n")) != "yes" {
					continue
				}
			}
			return DriveChoice{Path: suggestedPathFor(info)}, nil
		}
		fmt.Fprintf(out, "  unrecognised choice %q, please try again.\n", choice)
	}
}

// findDrive looks up a drive by letter (case-insensitive) in a list.
func findDrive(list []config.DriveInfo, letter string) (config.DriveInfo, bool) {
	upper := strings.ToUpper(letter)
	for _, d := range list {
		if strings.ToUpper(d.Letter) == upper {
			return d, true
		}
	}
	return config.DriveInfo{}, false
}

// suggestedPathFor returns <drive>\XDev.
func suggestedPathFor(d config.DriveInfo) string {
	return d.Path + "XDev"
}

// defaultUserHome returns %USERPROFILE%\.xdev.
func defaultUserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// We append under the *current* OS path separator.  On Windows
	// os.UserHomeDir returns the user-profile path with backslashes
	// already.
	sep := string(os.PathSeparator)
	return joinPath(home, ".xdev", sep)
}

func joinPath(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i == 0 {
			out = p
			continue
		}
		out = trimSep(out) + p
	}
	return out
}

func trimSep(s string) string {
	return strings.TrimRight(s, "/\\")
}
