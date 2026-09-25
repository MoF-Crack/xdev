package downloader

// 第六轮审计 F5 守卫：L-10「无哈希拒绝门」的可达性与过渡期策略。
//
// 事实链（审计实证）：门控条件为
//
//	HasRootKey() && !IsThirdParty && !AllowPlaceholderTOFU && !UnsignedVersion
//
// 而两个生产调用方（internal/installer/installer.go、internal/gui/app_install.go）
// 都把 AllowPlaceholderTOFU 派生为「哈希为空/占位」——恰好是门要拒绝的场景，
// 因此 keyed 构建对**已签名工具的空哈希版本**仍走 TOFU 兜底（未签名工具则在
// 更早的「签名缺失门」直接拒绝，与本门无关）。这是票 17 数据线（163 个空哈希
// 待填充+重签）完成前的**有意过渡策略**；本测试以与生产一致的 Options 派生
// 方式把该语义钉死：
//
//   - 过渡期（现状）：已签名工具 + 空哈希 + AllowPlaceholderTOFU=true →
//     TOFU 放行 + 写 sidecar；
//   - 端态（数据线完成后，票 17 卡片已登记）：调用方应传 false → 本测试必须
//     同步翻转断言为「拒绝」，改红是提醒策略闭环的机制，不是回归。
//
// 同时钉死反向事实：同一 keyed 构建下，若调用方按端态语义传
// AllowPlaceholderTOFU=false + 空哈希 → 下载必须被 L-10 门拒绝（门本身可达
// 且有效，不是死代码装饰）。两个子测试都挂**有效签名**（签的是另一个版本），
// 确保到达的确实是 L-10 门而不是更早的「签名缺失门」。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaceholderTOFU_TransitionalPolicy(t *testing.T) {
	ts := injectTestRootKey(t)

	payload := []byte("placeholder-era payload")
	srv := routesServer(t, map[string][]byte{"/p.zip": payload})

	// 与生产同形：工具已签名（签的是 0.9.0），1.0.0 是后来新增/空哈希版本。
	// 签名验证通过后，空的 ExpectedSHA 让覆盖过滤器跳过（238 行条件不满足），
	// 流程直达 L-10 空哈希分支——这正是 163 个空哈希条目在 keyed 构建下的形态。
	sig := ts.sign(t, "demo", map[string]map[string]string{
		"0.9.0": {"windows/amd64": sha256Of(payload)},
	})

	t.Run("production derivation allows empty-hash TOFU (transitional)", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "p.zip")
		err := Download(context.Background(), Options{
			URL: srv.URL + "/p.zip", Dest: dest, Client: srv.Client(),
			// 与 installer.go / app_install.go 的生产派生完全一致：
			// AllowPlaceholderTOFU: a.SHA256 == "" || util.IsAllZeroHash(a.SHA256)
			ExpectedSHA:          "",
			AllowPlaceholderTOFU: true,
			ManifestSignature:    sig,
			IsThirdParty:         false,
			UnsignedVersion:      false,
			ToolName:             "demo", Version: "1.0.0", OSArch: "windows/amd64",
		})
		if err != nil {
			t.Fatalf("keyed build must keep the transitional TOFU path for empty hashes: %v", err)
		}
		sidecar, rerr := os.ReadFile(dest + ".sha256")
		if rerr != nil || !strings.Contains(string(sidecar), sha256Of(payload)) {
			t.Errorf("TOFU sidecar pin missing/incorrect: err=%v content=%q", rerr, string(sidecar))
		}
	})

	t.Run("end-state refusal gate is reachable when caller opts out", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "p2.zip")
		err := Download(context.Background(), Options{
			URL: srv.URL + "/p.zip", Dest: dest, Client: srv.Client(),
			ExpectedSHA:          "",
			AllowPlaceholderTOFU: false, // 端态语义：不给占位哈希放行
			ManifestSignature:    sig,
			IsThirdParty:         false,
			UnsignedVersion:      false,
			ToolName:             "demo", Version: "1.0.0", OSArch: "windows/amd64",
		})
		if err == nil {
			t.Fatal("with AllowPlaceholderTOFU=false the L-10 gate must refuse an empty-hash download in keyed builds")
		}
		if !strings.Contains(err.Error(), "refusing to download without a verified hash") {
			t.Errorf("expected the L-10 gate (not the earlier signature-missing gate) to fire, got: %v", err)
		}
		if _, serr := os.Stat(dest); serr == nil {
			t.Error("refused download must not leave a dest artifact")
		}
	})
}
