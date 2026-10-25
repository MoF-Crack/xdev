package downloader

import (
	"net"
	"net/url"
	"strings"
	"testing"
)

// 审查 S-10 回归：下载 URL 必须拦截「解析到内网私有地址」的情况（SSRF），
// 但必须放行回环地址（本机服务/测试服务器是合法下载源）。
func TestDownloadURLBlocksPrivateNetwork(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
		// 期望错误里出现的关键字（wantErr 时）
		wantSub string
	}{
		{
			name:    "内网私网地址应拒绝",
			url:     "https://10.0.0.5/pkg.zip",
			wantErr: true,
			wantSub: "private network",
		},
		{
			name:    "192.168 段应拒绝",
			url:     "https://192.168.1.10/pkg.zip",
			wantErr: true,
			wantSub: "private network",
		},
		{
			name:    "172.16 段应拒绝",
			url:     "https://172.16.0.1/pkg.zip",
			wantErr: true,
			wantSub: "private network",
		},
		{
			name:    "云元数据链路本地地址应拒绝",
			url:     "https://169.254.169.254/latest/meta-data",
			wantErr: true,
			wantSub: "private network",
		},
		{
			name:    "回环地址应放行（本机服务合法）",
			url:     "https://127.0.0.1:8443/pkg.zip",
			wantErr: false,
		},
		{
			name:    "localhost 应放行",
			url:     "https://localhost:8443/pkg.zip",
			wantErr: false,
		},
		{
			name:    "无法解析的域名应放行（交给实际请求报错）",
			url:     "https://this-host-does-not-exist.invalid/pkg.zip",
			wantErr: false,
		},
		{
			name:    "非 https 仍应拒绝",
			url:     "http://10.0.0.5/pkg.zip",
			wantErr: true,
			wantSub: "HTTPS",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, perr := url.Parse(c.url)
			if perr != nil {
				t.Fatalf("url.Parse(%q): %v", c.url, perr)
			}
			err := validateDownloadURL(u)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望拒绝 %s，但通过了", c.url)
				}
				if c.wantSub != "" && !strings.Contains(err.Error(), c.wantSub) {
					t.Errorf("错误信息未包含 %q: %v", c.wantSub, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望放行 %s，但被拒: %v", c.url, err)
			}
		})
	}
}

// 审查 S-13 回归：fallback URL 也必须经过 SSRF 校验。
//
// 背景：主 URL 在校验后，旧实现只对 fallback 检查了 scheme。于是
// 「主 URL 指向公网、fallback 指向内网」的 manifest 就能绕过 S-10 的防护，
// 而 fallback 是真正会被发请求的候选之一。
func TestFallbackURLsAreSSRFChecked(t *testing.T) {
	// 直接验证过滤逻辑：内网 fallback 不应进入候选列表。
	// 这里通过 validateDownloadURL 的行为间接断言（Download 内部对每个 fallback
	// 调用同一函数，不符的会被 continue 跳过）。
	unsafeFallbacks := []string{
		"https://10.0.0.5/pkg.zip",
		"https://192.168.1.10/pkg.zip",
		"https://172.16.0.1/pkg.zip",
		"https://169.254.169.254/latest/meta-data",
	}
	for _, fb := range unsafeFallbacks {
		u, err := url.Parse(fb)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", fb, err)
		}
		if verr := validateDownloadURL(u); verr == nil {
			t.Errorf("fallback %q 未被 SSRF 拦截 —— 会绕过主 URL 的校验", fb)
		}
	}

	// 回环 fallback 仍应放行（本地镜像/测试服务器）。
	safeFallbacks := []string{
		"https://127.0.0.1:8443/pkg.zip",
		"https://localhost:8443/pkg.zip",
	}
	for _, fb := range safeFallbacks {
		u, err := url.Parse(fb)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", fb, err)
		}
		if verr := validateDownloadURL(u); verr != nil {
			t.Errorf("回环 fallback %q 被误拦: %v", fb, verr)
		}
	}
}

// TestIsPrivateNetworkIP 单元测试：回环不算「内网私网」。
func TestIsPrivateNetworkIP(t *testing.T) {
	reject := []string{"10.0.0.1", "192.168.0.1", "172.16.0.1", "169.254.1.1", "0.0.0.0"}
	for _, s := range reject {
		if !isPrivateNetworkIP(net.ParseIP(s)) {
			t.Errorf("isPrivateNetworkIP(%s) = false, want true", s)
		}
	}
	allow := []string{"127.0.0.1", "::1", "8.8.8.8", "1.1.1.1"}
	for _, s := range allow {
		if isPrivateNetworkIP(net.ParseIP(s)) {
			t.Errorf("isPrivateNetworkIP(%s) = true, want false（回环/公网应放行）", s)
		}
	}
}
