package manifest

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/xmdemt/xdev/internal/util"
)

// ResolveWritableToolPath uses the same exact two-level disk inventory and
// precedence as catalog loading. It never chooses a repository fallback as a
// user write target. Embedded-only tools get a new top-level disk overlay.
// Callers must recheck this result while holding their write lock before merge.
func ResolveWritableToolPath(dir, toolName string) (string, error) {
	if _, err := util.SafeName(toolName); err != nil {
		return "", fmt.Errorf("invalid tool name for manifest write: %w", err)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("manifest write root must not be empty")
	}
	var selected string
	if info, err := os.Stat(root); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("manifest root is not a directory")
		}
		files, err := collectSource(os.DirFS(root), "disk")
		if err != nil {
			return "", err
		}
		for _, file := range files {
			tool, err := loadOneFile(path.Base(file.relative), func() ([]byte, error) { return file.data, file.readErr })
			if err != nil {
				return "", fmt.Errorf("resolve writable manifest %s: %w", file.relative, err)
			}
			if tool.Name == toolName {
				selected = filepath.Join(root, filepath.FromSlash(file.relative))
			}
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if selected == "" {
		selected = filepath.Join(root, toolName+".yaml")
	}
	if err := util.ValidateSubPath(root, selected); err != nil {
		return "", fmt.Errorf("writable manifest escapes root: %w", err)
	}
	return selected, nil
}
