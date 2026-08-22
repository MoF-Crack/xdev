package isolator

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// persistedState is the on-disk isolator session (Spec 032). It lets
// `xdev isolate setup` and `xdev isolate exec` share state across
// processes instead of losing the container ID / flake dir.
type persistedState struct {
	ContainerID string `json:"containerId,omitempty"`
	FlakeDir    string `json:"flakeDir,omitempty"`
	ProjectDir  string `json:"projectDir,omitempty"`
	Image       string `json:"image,omitempty"`
}

func statePath(xmdeDir, mode string) string {
	return filepath.Join(xmdeDir, "isolate", mode+".json")
}

func saveState(xmdeDir, mode string, s persistedState) {
	if xmdeDir == "" || mode == "" {
		return
	}
	p := statePath(xmdeDir, mode)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.Marshal(s)
	_ = os.WriteFile(p, data, 0o644)
}

func loadState(xmdeDir, mode string) persistedState {
	var s persistedState
	if xmdeDir == "" || mode == "" {
		return s
	}
	data, err := os.ReadFile(statePath(xmdeDir, mode))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s)
	return s
}

func clearState(xmdeDir, mode string) {
	if xmdeDir == "" || mode == "" {
		return
	}
	_ = os.Remove(statePath(xmdeDir, mode))
}
