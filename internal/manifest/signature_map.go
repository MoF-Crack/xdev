package manifest

import (
	"strings"

	"github.com/xmdemt/xdev/internal/util"
)

// BuildSignatureHashMap 收集一个工具用于签名的哈希图：version → 键 → sha256。
//
// 键有两种形态：
//
//  1. `os/arch`         —— 官方归档哈希（Archive.SHA256）；
//  2. `os/arch#mirror`  —— manifest 声明的镜像哈希（Archive.MirrorSHA256）。
//
// 为什么必须有第 2 种：下载前的逐 URL 签名覆盖过滤
// （internal/downloader.filterSignatureCoveredURLs）要求每个候选 URL 的**生效哈希**
// 出现在本版本的签名图里，否则该候选被剔除。java/Temurin 的镜像与官方**不是同一份
// 字节**（manifest 用 mirror_sha256 单独声明），因此只签官方哈希时镜像候选会被判
// 「未入签」而全部剔除——镜像通路形同失效（票 17 第 3 项）。
//
// 键名带 `#mirror` 后缀即可满足验证侧：downloader.verifySHAInMapForVersion 对**本
// 版本**先做 `os/arch` 精确键匹配，再做同版本内「任意键值匹配」（值扫描），因此本
// 函数不需要任何验证侧配合改动。
//
// 与官方哈希相同（忽略大小写）或占位（全零/空）的 mirror_sha256 不重复签，避免噪声；
// 只有镜像哈希而没有官方哈希时，镜像哈希本身仍算「可签的真实哈希」。
//
// 返回值 hasHash 表示该工具是否存在任何可用哈希；为 false 的工具（例如 npm 型的 dsh，
// 或尚未补全哈希的条目）无法签名，调用方应跳过——与「无哈希豁免」口径一致。
func BuildSignatureHashMap(tool *Tool) (map[string]map[string]string, bool) {
	sha256Map := map[string]map[string]string{}
	if tool == nil {
		return sha256Map, false
	}
	hasHash := false
	for _, v := range tool.VersionList {
		verMap := map[string]string{}
		for _, a := range v.Archives {
			key := a.OS + "/" + a.Arch
			officialUsable := a.SHA256 != "" && !util.IsAllZeroHash(a.SHA256)
			if officialUsable {
				verMap[key] = a.SHA256
				hasHash = true
			}
			mirror := a.MirrorSHA256
			if mirror != "" && !util.IsAllZeroHash(mirror) && !strings.EqualFold(mirror, a.SHA256) {
				verMap[key+"#mirror"] = mirror
				if !officialUsable {
					hasHash = true
				}
			}
		}
		if len(verMap) > 0 {
			sha256Map[v.Version] = verMap
		}
	}
	return sha256Map, hasHash
}

// SignatureCoversArchive reports whether a usable hash of the archive appears in
// the signed map under the given version — the invariant a post-resign manifest
// tree must satisfy (see the manifests-integrity guard).
//
// It mirrors the verifier's lookup semantics: exact `os/arch` key first, then a
// value scan within the SAME version (downloader.verifySHAInMapForVersion), so a
// `os/arch#mirror` entry counts as coverage for the mirror hash.
func SignatureCoversArchive(verMap map[string]string, osArch, hash string) bool {
	if len(verMap) == 0 || hash == "" || util.IsAllZeroHash(hash) {
		return true // 无可用哈希：不构成覆盖缺口（由「无哈希豁免」口径负责）
	}
	if osArch != "" {
		if h, ok := verMap[osArch]; ok && strings.EqualFold(h, hash) {
			return true
		}
	}
	for _, h := range verMap {
		if strings.EqualFold(h, hash) {
			return true
		}
	}
	return false
}
