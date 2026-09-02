package cli

import (
	"testing"

	"github.com/xmdemt/xdev/internal/util"
)

// TestPluginNameTraversalRejected guards the H-1/H-2 fix: plugin
// create/remove derive their file paths from the CLI arg — traversal
// payloads must fail util.SafeName so they can never write or delete
// outside $XMDEV_HOME/plugins/. (Quote-bearing names are separately
// rejected by the create command for Lua template safety — see
// plugin.go newPluginCreateCmd.)
func TestPluginNameTraversalRejected(t *testing.T) {
	payloads := []string{
		`..\..\..\victim`,
		`../../victim`,
		`..`,
		`sub/dir`,
		`C:\evil`,
		`CON`,
		`name ends with dot.`,
	}
	for _, p := range payloads {
		if _, err := util.SafeName(p); err == nil {
			t.Errorf("plugin name %q must be rejected by SafeName", p)
		}
	}
	// Valid names still pass.
	for _, p := range []string{"mytool", "my-tool", "mytool2"} {
		if _, err := util.SafeName(p); err != nil {
			t.Errorf("valid plugin name %q must pass: %v", p, err)
		}
	}
}