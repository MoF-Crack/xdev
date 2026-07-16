package project

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadToolVersions reads an asdf-style .tool-versions file and
// returns a ProjectConfig with the tools populated.
//
// Format: one line per tool, "<name> <version>"
// Lines starting with # are comments. Blank lines are ignored.
func LoadToolVersions(path string) (*ProjectConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &ProjectConfig{
		Tools: map[string]string{},
		Env:   map[string]string{},
		Tasks: map[string]Task{},
	}

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue // skip malformed lines
		}
		name := parts[0]
		ver := parts[1]
		// asdf allows multiple versions separated by spaces
		// e.g. "python 3.12.0 3.11.0" — we take the first one
		cfg.Tools[name] = ver
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}
