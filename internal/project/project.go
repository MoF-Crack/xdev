// Package project handles .xdev.toml project-level configuration files.
//
// A .xdev.toml file lives at the root of a project and declares which
// tool versions the project needs.  When the user cd's into the
// project directory (with cd hook enabled), xdev automatically
// switches to the versions listed in the file.
//
// The file format is TOML:
//
//	[tools]
//	node = "20"
//	python = "^3.12"
//	go = "1.22"
//
//	[env]
//	DATABASE_URL = "postgres://localhost/app"
//
//	[tasks.build]
//	run = "go build ./..."
//	desc = "Build the project"
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const configFileName = ".xdev.toml"

// ProjectConfig represents the contents of a .xdev.toml file.
type ProjectConfig struct {
	Tools map[string]string `toml:"tools"`
	Env   map[string]string `toml:"env"`
	Tasks map[string]Task   `toml:"tasks"`
}

// Task represents a named command in the project config.
type Task struct {
	Run       string   `toml:"run"`
	Desc      string   `toml:"desc"`
	Dir       string   `toml:"dir,omitempty"`
	Sources   []string `toml:"sources,omitempty"`
	DependsOn []string `toml:"depends_on,omitempty"`
}

// FindConfig searches from dir upward for a .xdev.toml file.
// Returns the directory containing the file and the parsed config,
// or ("", nil) if none is found.
func FindConfig(dir string) (string, *ProjectConfig) {
	for {
		p := filepath.Join(dir, configFileName)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			cfg, err := LoadConfig(p)
			if err == nil {
				return dir, cfg
			}
		}
		// Also check for .tool-versions (asdf compatibility)
		tvPath := filepath.Join(dir, ".tool-versions")
		if fi, err := os.Stat(tvPath); err == nil && !fi.IsDir() {
			cfg, err := LoadToolVersions(tvPath)
			if err == nil && cfg != nil && len(cfg.Tools) > 0 {
				return dir, cfg
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", nil
}

// LoadConfig reads and parses a .xdev.toml file.
func LoadConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &ProjectConfig{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Tools == nil {
		cfg.Tools = map[string]string{}
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if cfg.Tasks == nil {
		cfg.Tasks = map[string]Task{}
	}
	return cfg, nil
}

// SaveConfig writes a ProjectConfig to the given path.
func SaveConfig(path string, cfg *ProjectConfig) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SetTool adds or updates a tool version in the config and saves it.
// If the config file doesn't exist, it creates one in the given directory.
func SetTool(dir, tool, version string) error {
	path := filepath.Join(dir, configFileName)
	cfg := &ProjectConfig{Tools: map[string]string{}, Env: map[string]string{}, Tasks: map[string]Task{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(data, cfg)
		if cfg.Tools == nil {
			cfg.Tools = map[string]string{}
		}
		if cfg.Env == nil {
			cfg.Env = map[string]string{}
		}
		if cfg.Tasks == nil {
			cfg.Tasks = map[string]Task{}
		}
	}
	cfg.Tools[tool] = version
	return SaveConfig(path, cfg)
}

// UnsetTool removes a tool from the config and saves it.
func UnsetTool(dir, tool string) error {
	path := filepath.Join(dir, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(".xdev.toml not found in %s", dir)
	}
	cfg := &ProjectConfig{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return err
	}
	if cfg.Tools == nil {
		return fmt.Errorf("tool %s not found in .xdev.toml", tool)
	}
	if _, ok := cfg.Tools[tool]; !ok {
		return fmt.Errorf("tool %s not found in .xdev.toml", tool)
	}
	delete(cfg.Tools, tool)
	return SaveConfig(path, cfg)
}

// FormatTools returns a human-readable listing of the tools section.
func FormatTools(cfg *ProjectConfig) string {
	if len(cfg.Tools) == 0 {
		return "(no tools configured)"
	}
	var b strings.Builder
	for name, ver := range cfg.Tools {
		fmt.Fprintf(&b, "  %-20s %s\n", name, ver)
	}
	return b.String()
}
