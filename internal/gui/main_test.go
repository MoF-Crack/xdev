package gui

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/testkit/hostsnapshot"
)

// TestMain 固定测试用 xdev home 并启用宿主污染快照。
//
// gui 包此前没有 TestMain，也没有全局钉住 XMDEV_HOME，凡是走到
// shim.InvalidateShimIndex() → writeDiskShimIndex(config.ResolveHomeDir()) 或
// config.ResolveHomeDir() → writeHomeCache() 的用例都会直接改写开发者真实的
// xdev home（实测：E:\XDev\.home.cache 与 E:\XDev\cache\shim-index.v1 被
// TestToolCommand_UnknownTool / TestTestProxy_Failure_JSONFormat /
// TestUseVersion_NotInstalled_Rejected 三个用例改写）。
func TestMain(m *testing.M) {
	os.Exit(hostsnapshot.RunWithPinnedHome(m))
}
