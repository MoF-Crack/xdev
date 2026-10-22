package isolator

import (
	"os"
	"strings"
	"testing"
)

// 第七轮审计 F-50 守卫（源码级）：隔离环境**不得**注入宿主 PATH。
//
// 缺陷现场：spec.Env["PATH"] 由 cli/isolate.go 组装为「宿主 shim 目录 + 宿主 PATH」，
// 该语义只对 `none` 模式成立（noneIsolator 就地 os.Setenv）。container.go 把它作为
// `-e PATH=...` 传给 docker，nix.go 把它写进 flake 的 env：
//   - 容器里该值指向宿主路径，而挂载只有 projectDir —— Windows 主机的分号分隔
//     PATH 在 Linux 容器里是单个无效条目，镜像自带的 sleep 无法经 PATH 解析，
//     docker run 直接失败（runc: executable file not found in $PATH）；
//   - nix flake 里它会**覆盖** Nix 装配的 profile PATH，使声明的 pkgs 从 PATH 消失。
//
// 两处都必须显式跳过 PATH 键。
func TestF50_IsolatorsDoNotInjectHostPath(t *testing.T) {
	for _, tc := range []struct {
		file string
	}{
		{"container.go"},
		{"nix.go"},
	} {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		s := string(data)

		if !strings.Contains(s, `strings.EqualFold(k, "PATH")`) {
			t.Errorf("F-50 复现：%s 未跳过 PATH 键 —— 宿主 PATH 会被注入隔离环境", tc.file)
		}
		// 守卫必须真的能拦住注入：出现 continue 且带 F-50 说明。
		if !strings.Contains(s, "F-50") {
			t.Errorf("%s: expected an F-50 explanation next to the PATH skip", tc.file)
		}
	}
}
