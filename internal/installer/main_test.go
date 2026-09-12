package installer

import (
	"os"
	"testing"

	"github.com/xmdemt/xdev/internal/testkit/hostsnapshot"
)

// TestMain 固定测试用 xdev home 并启用宿主污染快照（见
// hostsnapshot.RunWithPinnedHome 注释：ResolveHomeDir 的 sync.Once 会把
// 首次解析到的真实 home 粘住整包测试，导致后续 shim 索引/缓存写入真实 home）。
func TestMain(m *testing.M) {
	os.Exit(hostsnapshot.RunWithPinnedHome(m))
}
