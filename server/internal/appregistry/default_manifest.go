package appregistry

import _ "embed"

// 内嵌缺省清单(PROVISIONAL 样例;与 public-ai sample-manifest.json 同构)。
// URL 一律 *.example.invalid(永不解析);真实部署经 PRODUCT_REGISTRY_MANIFEST
// 指向平台登记表(生产值由各接入工单决定)。本应用只消费 target 解析,
// enabled 语义归来源平台(E1 静态事实),本包不据此放行任何调用。
//
//go:embed default_manifest.json
var defaultManifestRaw []byte

// DefaultManifest 加载内嵌缺省清单(加载即校验;失败为部署错误)。
func DefaultManifest() (*Manifest, error) {
	return LoadManifest(defaultManifestRaw)
}
