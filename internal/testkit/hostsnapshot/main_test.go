package hostsnapshot

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(RunWithHostSnapshot(m, DefaultOptions()))
}
