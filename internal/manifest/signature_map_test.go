// signature_map_test.go — 票 17 第 3 项守卫：签名图的 `os/arch#mirror` 键。
//
// 背景：java/Temurin 的镜像与官方不是同一份字节（manifest 用 mirror_sha256 单独
// 声明）。签名图若只包含官方哈希，下载前的逐 URL 覆盖过滤会把镜像候选判为
// 「未入签」而剔除（镜像通路失效）。本文件锁定收集规则；
// 「验证侧确实消费该键」由 internal/downloader/signature_gate_test.go 的同名守卫
// 以真实过滤函数端到端证明。
package manifest

import "testing"

const (
	officialHex = "1111111111111111111111111111111111111111111111111111111111111111"
	mirrorHex   = "2222222222222222222222222222222222222222222222222222222222222222"
)

func toolWithArchives(versions ...Version) *Tool {
	return &Tool{Name: "java", VersionList: versions}
}

func arch(osName, arch, sha, mirrorSHA string) Archive {
	return Archive{OS: osName, Arch: arch, SHA256: sha, MirrorSHA256: mirrorSHA}
}

// TestBuildSignatureHashMap_MirrorKeyAdded：mirror_sha256 与官方不同时必须额外出
// 一个 `os/arch#mirror` 键（票 17 第 3 项的核心）。
func TestBuildSignatureHashMap_MirrorKeyAdded(t *testing.T) {
	m, has := BuildSignatureHashMap(toolWithArchives(Version{
		Version:  "21.0.1",
		Archives: []Archive{arch("linux", "amd64", officialHex, mirrorHex)},
	}))
	if !has {
		t.Fatal("hasHash = false, want true（有官方哈希）")
	}
	got := m["21.0.1"]
	if got["linux/amd64"] != officialHex {
		t.Errorf("linux/amd64 = %q, want official", got["linux/amd64"])
	}
	if got["linux/amd64#mirror"] != mirrorHex {
		t.Errorf("linux/amd64#mirror = %q, want mirror %q —— 缺失则该镜像候选会被过滤剔除", got["linux/amd64#mirror"], mirrorHex)
	}
	if len(got) != 2 {
		t.Errorf("keys = %v, want exactly 2", got)
	}
}

// TestBuildSignatureHashMap_MirrorKeyOmittedWhenRedundant：与官方相同（含大小写
// 差异）或占位（全零/空）的 mirror_sha256 不应产生冗余键。
func TestBuildSignatureHashMap_MirrorKeyOmittedWhenRedundant(t *testing.T) {
	cases := []struct {
		name   string
		mirror string
	}{
		{"empty", ""},
		{"identical", officialHex},
		{"identical-uppercase", "1111111111111111111111111111111111111111111111111111111111111111"},
		{"all-zero", "0000000000000000000000000000000000000000000000000000000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := BuildSignatureHashMap(toolWithArchives(Version{
				Version:  "21.0.1",
				Archives: []Archive{arch("linux", "amd64", officialHex, tc.mirror)},
			}))
			got := m["21.0.1"]
			if _, ok := got["linux/amd64#mirror"]; ok {
				t.Errorf("冗余 mirror 键被写入: %v", got)
			}
			if len(got) != 1 {
				t.Errorf("keys = %v, want only the official key", got)
			}
		})
	}
}

// TestBuildSignatureHashMap_NoHashToolIsSkipped：完全无哈希的工具（npm 型 dsh、
// 或尚未补全哈希的条目）hasHash 必须为 false —— 调用方据此跳过签名，
// 与「无哈希豁免」口径对齐。
func TestBuildSignatureHashMap_NoHashToolIsSkipped(t *testing.T) {
	m, has := BuildSignatureHashMap(toolWithArchives(
		Version{Version: "1.0.0", Archives: []Archive{arch("linux", "amd64", "", "")}},
		Version{Version: "1.0.1", Archives: []Archive{arch("linux", "amd64", "0000000000000000000000000000000000000000000000000000000000000000", "")}},
	))
	if has {
		t.Error("hasHash = true, want false（空/全零哈希不算可用哈希）")
	}
	if len(m) != 0 {
		t.Errorf("map = %v, want empty", m)
	}
}

// TestBuildSignatureHashMap_MirrorOnlyHashIsSignable：只有镜像哈希（官方哈希缺失）
// 也算可签的真实哈希，否则该条目会被整条跳过、镜像通路无签名可依。
func TestBuildSignatureHashMap_MirrorOnlyHashIsSignable(t *testing.T) {
	m, has := BuildSignatureHashMap(toolWithArchives(Version{
		Version:  "1.0.0",
		Archives: []Archive{arch("linux", "amd64", "", mirrorHex)},
	}))
	if !has {
		t.Error("hasHash = false, want true（镜像哈希可用）")
	}
	if m["1.0.0"]["linux/amd64#mirror"] != mirrorHex {
		t.Errorf("map = %v, want the mirror key only", m)
	}
}

// TestBuildSignatureHashMap_MultiVersionAndPlatform：多版本 × 多平台各自成键，
// 且镜像键始终与官方键处于**同一版本**之下（验证侧按版本内值扫描）。
func TestBuildSignatureHashMap_MultiVersionAndPlatform(t *testing.T) {
	v2mirror := "3333333333333333333333333333333333333333333333333333333333333333"
	m, has := BuildSignatureHashMap(toolWithArchives(
		Version{Version: "21.0.1", Archives: []Archive{
			arch("linux", "amd64", officialHex, mirrorHex),
			arch("windows", "amd64", officialHex, ""),
		}},
		Version{Version: "22.0.0", Archives: []Archive{
			arch("darwin", "arm64", officialHex, v2mirror),
		}},
	))
	if !has {
		t.Fatal("hasHash = false, want true")
	}
	if got := m["21.0.1"]["linux/amd64#mirror"]; got != mirrorHex {
		t.Errorf("21.0.1 mirror key = %q, want %q", got, mirrorHex)
	}
	if _, ok := m["21.0.1"]["windows/amd64#mirror"]; ok {
		t.Error("windows/amd64 没有独立镜像哈希，不应有 #mirror 键")
	}
	if got := m["22.0.0"]["darwin/arm64#mirror"]; got != v2mirror {
		t.Errorf("22.0.0 mirror key = %q, want %q（镜像键必须挂在同一版本下）", got, v2mirror)
	}
}

// TestSignatureCoversArchive：覆盖判定语义（精确键 → 同版本值扫描；无可用哈希
// 不算缺口），供重签后的完整性守卫复用。
func TestSignatureCoversArchive(t *testing.T) {
	verMap := map[string]string{
		"linux/amd64":        officialHex,
		"linux/amd64#mirror": mirrorHex,
	}
	if !SignatureCoversArchive(verMap, "linux/amd64", officialHex) {
		t.Error("官方哈希应被判定为已覆盖")
	}
	if !SignatureCoversArchive(verMap, "linux/amd64", mirrorHex) {
		t.Error("镜像哈希（同版本值扫描）应被判定为已覆盖")
	}
	if SignatureCoversArchive(verMap, "linux/amd64", "4444444444444444444444444444444444444444444444444444444444444444") {
		t.Error("未入签的哈希不应被判定为已覆盖")
	}
	if !SignatureCoversArchive(verMap, "windows/amd64", "") {
		t.Error("空哈希不构成覆盖缺口（无哈希豁免口径）")
	}
	if !SignatureCoversArchive(verMap, "windows/amd64", "0000000000000000000000000000000000000000000000000000000000000000") {
		t.Error("占位哈希不构成覆盖缺口")
	}
}
