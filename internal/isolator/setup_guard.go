package isolator

import (
	"fmt"
	"os"
)

// ensureOverwritable is the C-07 / P1-34 existence gate for the files Setup
// generates (flake.nix, .devcontainer/devcontainer.json).
//
// Before this change Setup overwrote whatever was there and Teardown then
// deleted it, so a project with a hand-written flake.nix or devcontainer.json
// lost it twice over. A pre-existing file belongs to the user: refuse instead
// of replacing it, and let an explicit `--force` (= EnvironmentSpec.Force)
// opt into the overwrite.
//
// Unreadable/undetermined targets fail closed (the error propagates).
func ensureOverwritable(path string, force bool) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("check %s: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%s exists and is a directory; remove it to let xdev generate the file", path)
	}
	if force {
		return nil
	}
	return fmt.Errorf("%s already exists; refusing to overwrite it (re-run with --force to replace the existing file)", path)
}
