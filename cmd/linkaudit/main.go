package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmdemt/xdev/internal/linkaudit"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("linkaudit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifests := flags.String("manifests", "./manifests", "directory containing YAML manifests")
	platforms := flags.String("platform", "", "comma-separated os/arch platforms")
	tools := flags.String("tool", "", "comma-separated tool names (exact match)")
	versions := flags.String("version", "", "comma-separated versions (exact match)")
	roles := flags.String("role", "", "comma-separated source roles: official or mirror")
	mode := flags.String("mode", string(linkaudit.ModeProbe), "audit mode: probe or full")
	cache := flags.String("cache", filepath.Join(os.TempDir(), "xdev-linkaudit"), "full-download cache directory")
	timeout := flags.Duration("timeout", 30*time.Second, "timeout per source")
	concurrency := flags.Int("concurrency", 4, "maximum concurrent source checks")
	report := flags.String("report", "", "report path (.json or .md)")
	jsonPath := flags.String("json", "", "JSON report path")
	markdownPath := flags.String("markdown", "", "Markdown report path")
	failDeterministic := flags.Bool("fail-on-deterministic", false, "exit non-zero when deterministic failures are reported")
	ciGate := flags.Bool("ci-gate", false, "apply the release gate from docs/download-source-policy.md and exit non-zero when any official source is dead or transient")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return 0
	} else if err != nil {
		return 2
	}

	if *report != "" {
		switch strings.ToLower(filepath.Ext(*report)) {
		case ".json":
			*jsonPath = *report
		case ".md", ".markdown":
			*markdownPath = *report
		default:
			fmt.Fprintln(stderr, "-report must end in .json, .md, or .markdown")
			return 2
		}
	}
	parseList := func(input string) []string {
		var selected []string
		for _, value := range strings.Split(input, ",") {
			if value = strings.TrimSpace(value); value != "" {
				selected = append(selected, value)
			}
		}
		return selected
	}
	selected := parseList(*platforms)
	results, err := linkaudit.Audit(context.Background(), linkaudit.Request{ManifestDir: *manifests, Platforms: selected, Tools: parseList(*tools), Versions: parseList(*versions), Roles: parseList(*roles), Mode: linkaudit.Mode(*mode), CacheDir: *cache, Timeout: *timeout, Concurrency: *concurrency})
	if err != nil {
		fmt.Fprintf(stderr, "audit: %v\n", err)
		return 2
	}
	if *jsonPath == "" && *markdownPath == "" {
		if err := linkaudit.WriteJSON(stdout, results); err != nil {
			fmt.Fprintf(stderr, "write report: %v\n", err)
			return 2
		}
	}
	if *jsonPath != "" {
		if err := writeReport(*jsonPath, func(w io.Writer) error { return linkaudit.WriteJSON(w, results) }); err != nil {
			fmt.Fprintf(stderr, "write JSON: %v\n", err)
			return 2
		}
	}
	if *markdownPath != "" {
		if err := writeReport(*markdownPath, func(w io.Writer) error { return linkaudit.WriteMarkdown(w, results) }); err != nil {
			fmt.Fprintf(stderr, "write Markdown: %v\n", err)
			return 2
		}
	}
	if *ciGate {
		outcome := linkaudit.CIGate(results)
		outcome.Print(stderr)
		return outcome.ExitCode
	}
	if *failDeterministic {
		for _, result := range results {
			if deterministicFailure(result.FailureReason) {
				return 1
			}
		}
	}
	return 0
}

func writeReport(path string, write func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if err := write(file); err != nil {
		file.Close()
		_ = os.Remove(file.Name())
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return err
	}
	backup := path + ".old"
	hadOld := false
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(backup)
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(file.Name())
			return err
		}
		hadOld = true
	}
	if err := os.Rename(file.Name(), path); err != nil {
		_ = os.Remove(file.Name())
		if hadOld {
			_ = os.Rename(backup, path)
		}
		return err
	}
	if hadOld {
		_ = os.Remove(backup)
	}
	return nil
}

func deterministicFailure(status string) bool {
	switch status {
	case linkaudit.StatusURLDead, linkaudit.StatusMirrorDead, linkaudit.StatusHashMismatch, linkaudit.StatusHTMLInsteadOfArchive, linkaudit.StatusUnsupportedLayout, linkaudit.StatusAuthOrLicenseRequired, linkaudit.StatusImplementationMissing, linkaudit.StatusMissingChecksum:
		return true
	default:
		return false
	}
}
