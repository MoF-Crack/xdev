package cli

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/testkit/hostsnapshot"
)

// TestMain 把整包测试（含子进程）的 xdev home 钉到临时目录后再跑，
// 并启用宿主污染快照。理由见 hostsnapshot.RunWithPinnedHome 的注释：
// config.ResolveHomeDir 用 sync.Once 记忆首次解析结果，只要有一次在
// XMDEV_HOME 缺失时被调用，真实 xdev home 就会被写缓存并「粘住」整包测试
// （本批次已真实发生过：真实 E:\XDev\config.yaml 被测试覆盖）。
func TestMain(m *testing.M) {
	os.Exit(hostsnapshot.RunWithPinnedHome(m))
}
