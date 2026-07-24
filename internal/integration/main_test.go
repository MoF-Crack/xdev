//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/testkit/hostsnapshot"
)

func TestMain(m *testing.M) {
	// Suite-level host snapshot for Spec 005 / Constitution principle II.
	// Default go test ./internal/... does not build this file (integration tag).
	os.Exit(hostsnapshot.RunWithHostSnapshot(m, hostsnapshot.DefaultOptions()))
}
