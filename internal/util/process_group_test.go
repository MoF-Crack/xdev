//go:build !windows

package util

import (
	"os/exec"
	"testing"
)

// TestTrackProcessGroup_SetsPgid covers L15: cancellation must kill the whole
// process group, otherwise installer grandchildren survive Ctrl-C.
func TestTrackProcessGroup_SetsPgid(t *testing.T) {
	cmd := exec.Command("true")
	TrackProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("TrackProcessGroup must set Setpgid")
	}
	if cmd.Cancel == nil {
		t.Fatal("TrackProcessGroup must install a Cancel func")
	}
}
