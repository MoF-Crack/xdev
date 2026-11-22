package discovery

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Every crontab invocation goes to this owned test subprocess. No system cron
// executable is ever started, including on Linux.
func TestCronFixtureProcess(t *testing.T) {
	if os.Getenv("XDEV_CRON_FIXTURE") != "1" {
		return
	}
	path := os.Getenv("XDEV_CRON_FILE")
	if os.Args[len(os.Args)-1] == "-l" {
		switch os.Getenv("XDEV_CRON_READ") {
		case "denied":
			fmt.Fprintln(os.Stderr, "crontab: permission denied")
			os.Exit(1)
		case "missing":
			fmt.Fprintln(os.Stderr, "no crontab for fixture")
			os.Exit(1)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(b)
	} else {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			os.Exit(4)
		}
	}
	os.Exit(0)
}

func cronFixture(t *testing.T, initial string) (*linuxCron, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crontab")
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	prev := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name != "crontab" {
			panic("unexpected executable: " + name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		t.Cleanup(cancel)
		argv := append([]string{"-test.run=^TestCronFixtureProcess$", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], argv...)
		cmd.Env = append(os.Environ(), "XDEV_CRON_FIXTURE=1", "XDEV_CRON_FILE="+path)
		return cmd
	}
	t.Cleanup(func() { execCommand = prev })
	return &linuxCron{executable: func() (string, error) { return cronFixtureExe(t), nil }}, path
}

func cronFixtureExe(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return "/opt/xdev/bin/xdev"
	}
	exe, err := xdevPath()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestD08CronQuotedPathsAndStableMarker(t *testing.T) {
	for _, exe := range []string{"/opt/xdev folder/xdev", "/opt/user's bin/xdev", "/opt/50% tools/xdev"} {
		t.Run(exe, func(t *testing.T) {
			l, path := cronFixture(t, "# user\n")
			l.executable = func() (string, error) { return exe, nil }
			for i := 0; i < 2; i++ {
				if err := l.Install("03:00"); err != nil {
					t.Fatal(err)
				}
			}
			if st, err := l.Status(); err != nil || !st.Installed || st.Schedule != "0 3 * * *" {
				t.Fatalf("%+v %v", st, err)
			}
			l.executable = func() (string, error) { return "/new/location/renamed-binary", nil }
			if st, err := l.Status(); err != nil || !st.Installed {
				t.Fatalf("stable marker after move: %+v %v", st, err)
			}
			if err := l.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if got := readCronFixture(t, path); got != "# user\n" {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestD08CronCanonicalBasename(t *testing.T) {
	if os.Getenv("XDEV_REQUIRE_CANONICAL_CRON") != "1" {
		t.Skip("explicit canonical-binary run")
	}
	exe, err := xdevPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "xdev" {
		t.Fatalf("expected real basename=xdev: %q", exe)
	}
	t.Logf("real canonical executable=%q; crontab calls remain fixture-only", exe)
	TestD08CronRoundTripPreservesUserTasks(t)
}

func readCronFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestD08CronRoundTripPreservesUserTasks(t *testing.T) {
	exe := cronFixtureExe(t)
	user := "# xdev sniff --all --headless\nMAILTO=fixture@example.test\n\n" +
		"1 2 * * * echo xdev sniff --all --headless\n" +
		"2 2 * * * '/other/xdev' sniff --all --headless\n" +
		"3 2 * * * echo '# xdev-sniff:v1'\n" +
		"4 2 * * * " + exe + " sniff --all --headless --user-option\n\n"
	l, path := cronFixture(t, user)
	for i := 0; i < 2; i++ {
		if err := l.Install("03:00"); err != nil {
			t.Fatal(err)
		}
		body := readCronFixture(t, path)
		if !strings.HasPrefix(body, user) {
			t.Errorf("user crontab changed:\n%s", body)
		}
		if got := strings.Count(body, " # xdev-sniff:v1\n"); got != 1 {
			t.Errorf("managed entries=%d, want 1", got)
		}
		st, err := l.Status()
		if err != nil {
			t.Fatal(err)
		}
		if !st.Installed || st.Schedule != "0 3 * * *" {
			t.Errorf("status=%+v", st)
		}
	}
	if err := l.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if got := readCronFixture(t, path); got != user {
		t.Errorf("uninstall changed user cron:\nwant=%q\ngot=%q", user, got)
	}
	if st, err := l.Status(); err != nil || st.Installed {
		t.Errorf("status after uninstall=%+v err=%v", st, err)
	}
}

func TestD08CronMigratesOnlyExactLegacyCommand(t *testing.T) {
	exe := cronFixtureExe(t)
	quoted := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	for _, legacy := range []string{exe, quoted} {
		t.Run(legacy, func(t *testing.T) {
			user := "# keep this comment\n0 1 * * * echo xdev sniff\n"
			l, path := cronFixture(t, user+"5 4 * * 1 "+legacy+" sniff --all --headless\n")
			st, err := l.Status()
			if err != nil {
				t.Fatal(err)
			}
			if !st.Installed || st.Schedule != "5 4 * * 1" {
				t.Errorf("legacy status=%+v", st)
			}
			if err := l.Install("*/15 9-17 * * 1-5"); err != nil {
				t.Fatal(err)
			}
			if got := readCronFixture(t, path); !strings.HasPrefix(got, user) || strings.Count(got, " sniff --all --headless") != 1 {
				t.Errorf("migration=%q", got)
			}
			if err := l.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if got := readCronFixture(t, path); got != user {
				t.Errorf("legacy uninstall=%q", got)
			}
		})
	}
}

func TestD08CronReadFailureDoesNotOverwrite(t *testing.T) {
	for _, op := range []string{"install", "uninstall", "status"} {
		t.Run(op, func(t *testing.T) {
			const user = "# intact\n5 4 * * * echo xdev sniff\n"
			l, path := cronFixture(t, user)
			t.Setenv("XDEV_CRON_READ", "denied")
			var err error
			switch op {
			case "install":
				err = l.Install("")
			case "uninstall":
				err = l.Uninstall()
			case "status":
				_, err = l.Status()
			}
			if err == nil {
				t.Error("crontab read failure must be returned")
			}
			if got := readCronFixture(t, path); got != user {
				t.Errorf("read error overwrote cron: %q", got)
			}
		})
	}
}

func TestD08CronMissingIsDistinctFromFailure(t *testing.T) {
	l, path := cronFixture(t, "")
	t.Setenv("XDEV_CRON_READ", "missing")
	if st, err := l.Status(); err != nil || st.Installed {
		t.Fatalf("missing: %+v %v", st, err)
	}
	if err := l.Install(""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDEV_CRON_READ", "")
	if got := readCronFixture(t, path); !strings.Contains(got, " # xdev-sniff:v1\n") {
		t.Errorf("missing install=%q", got)
	}
}
