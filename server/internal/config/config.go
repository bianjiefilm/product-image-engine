package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 汇集全部运行时配置。平台密钥(BASE_URL/TOKEN)只允许出现在
// 本产品服务端 env(或 /etc/<产品>/<产品>.env),样例文件一律占位符,
// 真实值禁止入库。
type Config struct {
	Addr          string // PRODUCT_SERVER_ADDR,默认 127.0.0.1:18220
	DBPath        string // PRODUCT_DB_PATH,默认 ./data/product-image.db
	InternalToken string // PRODUCT_INTERNAL_TOKEN:web BFF → Go server 内部凭据

	// platform-identity(必需:登录/会话/JWKS)
	IdentityBaseURL string // PLATFORM_IDENTITY_BASE_URL
	IdentityAppID   string // PLATFORM_IDENTITY_APP_ID
	IdentityToken   string // PLATFORM_IDENTITY_TOKEN(IDENTITY_APP_TOKENS 里本 app 专用)
	IdentityIssuer  string // PLATFORM_IDENTITY_ISSUER(可选,配置后才校验 iss)

	// platform-upload / platform-task / platform-billing(I0 为 client+配置+fail-closed)
	UploadBaseURL  string // PLATFORM_UPLOAD_BASE_URL
	UploadToken    string // PLATFORM_UPLOAD_TOKEN
	TaskBaseURL    string // PLATFORM_TASK_BASE_URL
	TaskToken      string // PLATFORM_TASK_TOKEN
	BillingBaseURL string // PLATFORM_BILLING_BASE_URL
	BillingToken   string // PLATFORM_BILLING_TOKEN

	BillingEnabled    bool // ECO_BILLING_ENABLED,默认 off
	GenerationEnabled bool // FEATURE_GENERATION_ENABLED,默认 off

	// --- HUI-1745 I1:跨应用续接与成果回流 ---
	AppID string // PRODUCT_APP_ID,默认 product-image-engine(交接 target_app 校验/回执 source_app)
	// PRODUCT_REGISTRY_MANIFEST:app-registry/v1 清单文件路径(可选;
	// 缺省用内嵌样例,E2E 指向本地 stub 目标)。PRODUCT_RECEIPT_KEYS:
	// 回执 HMAC 专用密钥,格式 app_id:secret;...(对端来源 app 专用)。
	RegistryManifestPath string
	ReceiptKeys          string
	// PRODUCT_ORDER_INTERNAL_TOKEN:对端(guanlan-order,其 INTERNAL_TOKEN)
	// 内部通道 Bearer 令牌;非空时回执投递加发 Authorization: Bearer 头,
	// 为空时不发(接收端 401 走既有 failed/resend 兜底)。
	OrderInternalToken string

	// --- HUI-1703 FEAT-0204:电商尺寸适配(纯确定性,不触生成/计费) ---
	SizeAdaptEnabled bool // FEATURE_SIZE_ADAPT,默认 off(off 时尺寸适配路由不注册=404 不可见)
	// PRODUCT_SIZE_PRESETS:预设集 JSON 路径(可选;缺省用内嵌公开常见规格整理,
	// 商家可自定义覆盖;加载即校验,非法拒绝启动)。
	SizePresetsPath string

	// --- HUI-1697 FEAT-0198:产品照片上传(登记制开关,沿 size_adapt 语义) ---
	PhotoUploadEnabled bool // FEATURE_PHOTO_UPLOAD,默认 off(off 时照片上传路由不注册=404 不可见)
	// PRODUCT_PHOTO_MAX_BYTES:服务端单点大小上限,默认 20 MiB(配置化)。
	PhotoMaxBytes int64
}

// ReceiptKeyFor 返回对端来源 app 的回执 HMAC 密钥(fail-closed:未登记 → 空)。
func (c Config) ReceiptKeyFor(appID string) string {
	for _, part := range strings.Split(c.ReceiptKeys, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 && kv[0] == appID {
			return kv[1]
		}
	}
	return ""
}

// Load 从进程环境读取配置,补默认值。
func Load() Config {
	return Config{
		Addr:          getEnv("PRODUCT_SERVER_ADDR", "127.0.0.1:18220"),
		DBPath:        getEnv("PRODUCT_DB_PATH", "./data/product-image.db"),
		InternalToken: os.Getenv("PRODUCT_INTERNAL_TOKEN"),

		IdentityBaseURL: os.Getenv("PLATFORM_IDENTITY_BASE_URL"),
		IdentityAppID:   os.Getenv("PLATFORM_IDENTITY_APP_ID"),
		IdentityToken:   os.Getenv("PLATFORM_IDENTITY_TOKEN"),
		IdentityIssuer:  os.Getenv("PLATFORM_IDENTITY_ISSUER"),

		UploadBaseURL:  os.Getenv("PLATFORM_UPLOAD_BASE_URL"),
		UploadToken:    os.Getenv("PLATFORM_UPLOAD_TOKEN"),
		TaskBaseURL:    os.Getenv("PLATFORM_TASK_BASE_URL"),
		TaskToken:      os.Getenv("PLATFORM_TASK_TOKEN"),
		BillingBaseURL: os.Getenv("PLATFORM_BILLING_BASE_URL"),
		BillingToken:   os.Getenv("PLATFORM_BILLING_TOKEN"),

		BillingEnabled:    getBoolEnv("ECO_BILLING_ENABLED", false),
		GenerationEnabled: getBoolEnv("FEATURE_GENERATION_ENABLED", false),

		AppID:                getEnv("PRODUCT_APP_ID", "product-image-engine"),
		RegistryManifestPath: os.Getenv("PRODUCT_REGISTRY_MANIFEST"),
		ReceiptKeys:          os.Getenv("PRODUCT_RECEIPT_KEYS"),
		OrderInternalToken:   os.Getenv("PRODUCT_ORDER_INTERNAL_TOKEN"),

		SizeAdaptEnabled: getBoolEnv("FEATURE_SIZE_ADAPT", false),
		SizePresetsPath:  os.Getenv("PRODUCT_SIZE_PRESETS"),

		PhotoUploadEnabled: getBoolEnv("FEATURE_PHOTO_UPLOAD", false),
		PhotoMaxBytes:      getInt64Env("PRODUCT_PHOTO_MAX_BYTES", 20<<20),
	}
}

// FatalProblems 返回阻断性问题(目前是 identity 必需配置与内部凭据)。
// 存在致命问题时受保护端点一律 503,绝不回退。
func (c Config) FatalProblems() []string {
	var p []string
	if c.InternalToken == "" {
		p = append(p, "内部凭据未配置(PRODUCT_INTERNAL_TOKEN 为空)")
	}
	if c.IdentityBaseURL == "" {
		p = append(p, "平台身份服务地址未配置(PLATFORM_IDENTITY_BASE_URL 为空)")
	}
	if c.IdentityAppID == "" {
		p = append(p, "平台应用标识未配置(PLATFORM_IDENTITY_APP_ID 为空)")
	}
	if c.IdentityToken == "" {
		p = append(p, "平台身份专用令牌未配置(PLATFORM_IDENTITY_TOKEN 为空)")
	}
	return p
}

// GenerationUsable 判定“提交生成”动作是否放行。
// 返回 status=0 表示放行;否则为应答 HTTP 状态(503)与中文原因。fail-closed:宁可拒绝,不伪造成功。
func (c Config) GenerationUsable() (int, string) {
	if !c.GenerationEnabled {
		return 503, "生成能力开关未开启(FEATURE_GENERATION_ENABLED=0),生成任务不可用"
	}
	if c.TaskBaseURL == "" || c.TaskToken == "" {
		return 503, "生成任务服务未配置(缺少 PLATFORM_TASK_BASE_URL 或 PLATFORM_TASK_TOKEN)"
	}
	if c.UploadBaseURL == "" || c.UploadToken == "" {
		return 503, "素材上传服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN)"
	}
	return 0, ""
}

// BillingUsable 判定计费读取动作是否放行(同上 fail-closed)。
func (c Config) BillingUsable() (int, string) {
	if !c.BillingEnabled {
		return 503, "计费开关未开启(ECO_BILLING_ENABLED=0),费用信息不可用"
	}
	if c.BillingBaseURL == "" || c.BillingToken == "" {
		return 503, "计费服务未配置(缺少 PLATFORM_BILLING_BASE_URL 或 PLATFORM_BILLING_TOKEN)"
	}
	return 0, ""
}

// PhotoUploadUsable 判定照片上传动作是否放行(HUI-1697 FEAT-0198;fail-closed)。
// 开关 off 时路由根本不注册(404 不可见);on 但上传服务未配置 → 503 明确原因。
func (c Config) PhotoUploadUsable() (int, string) {
	if !c.PhotoUploadEnabled {
		return 503, "照片上传开关未开启(FEATURE_PHOTO_UPLOAD=0),上传能力不可用"
	}
	if c.UploadBaseURL == "" || c.UploadToken == "" {
		return 503, "素材上传服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN)"
	}
	return 0, ""
}

// PhotoUploadMaxBytes 返回生效的照片大小上限;零/负值回退默认 20MiB
// (防误配成 0 导致全部上传被拒)。
func (c Config) PhotoUploadMaxBytes() int64 {
	if c.PhotoMaxBytes > 0 {
		return c.PhotoMaxBytes
	}
	return 20 << 20
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getBoolEnv(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getInt64Env(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
