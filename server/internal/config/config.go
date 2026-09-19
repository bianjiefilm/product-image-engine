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
