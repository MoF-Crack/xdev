package installer

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/testkit/hostsnapshot"
)

func TestMain(m *testing.M) {
	os.Exit(hostsnapshot.RunWithHostSnapshot(m, hostsnapshot.DefaultOptions()))
}
