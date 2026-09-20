package config

import (
	"strings"
	"testing"
)

func setEnvs(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{
		"PRODUCT_SERVER_ADDR", "PRODUCT_DB_PATH", "PRODUCT_INTERNAL_TOKEN", "PRODUCT_ORDER_INTERNAL_TOKEN",
		"PLATFORM_IDENTITY_BASE_URL", "PLATFORM_IDENTITY_APP_ID", "PLATFORM_IDENTITY_TOKEN", "PLATFORM_IDENTITY_ISSUER",
		"PLATFORM_UPLOAD_BASE_URL", "PLATFORM_UPLOAD_TOKEN",
		"PLATFORM_TASK_BASE_URL", "PLATFORM_TASK_TOKEN",
		"PLATFORM_BILLING_BASE_URL", "PLATFORM_BILLING_TOKEN",
		"ECO_BILLING_ENABLED", "FEATURE_GENERATION_ENABLED",
	} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func fullEnv() map[string]string {
	return map[string]string{
		"PRODUCT_INTERNAL_TOKEN":     "it-test",
		"PLATFORM_IDENTITY_BASE_URL": "http://127.0.0.1:18101",
		"PLATFORM_IDENTITY_APP_ID":   "product-image",
		"PLATFORM_IDENTITY_TOKEN":    "idtok",
		"PLATFORM_UPLOAD_BASE_URL":   "http://127.0.0.1:18104",
		"PLATFORM_UPLOAD_TOKEN":      "uptok",
		"PLATFORM_TASK_BASE_URL":     "http://127.0.0.1:18103",
		"PLATFORM_TASK_TOKEN":        "tasktok",
		"PLATFORM_BILLING_BASE_URL":  "http://127.0.0.1:18102",
		"PLATFORM_BILLING_TOKEN":     "billtok",
	}
}

func TestDefaultsAreSafe(t *testing.T) {
	setEnvs(t, nil)
	c := Load()
	if c.Addr != "127.0.0.1:18220" {
		t.Fatalf("Addr 默认应为 127.0.0.1:18220, got %q", c.Addr)
	}
	if c.DBPath != "./data/product-image.db" {
		t.Fatalf("DBPath 默认错误: %q", c.DBPath)
	}
	if c.BillingEnabled || c.GenerationEnabled {
		t.Fatal("两个开关默认必须全 off")
	}
	if len(c.FatalProblems()) == 0 {
		t.Fatal("空配置必须有致命问题")
	}
}

func TestBoolParsing(t *testing.T) {
	setEnvs(t, map[string]string{"ECO_BILLING_ENABLED": "1", "FEATURE_GENERATION_ENABLED": "true"})
	c := Load()
	if !c.BillingEnabled || !c.GenerationEnabled {
		t.Fatal("1/true 应解析为 on")
	}
	setEnvs(t, map[string]string{"ECO_BILLING_ENABLED": "garbage"})
	if Load().BillingEnabled {
		t.Fatal("非法布尔应回退默认 off")
	}
}

func TestFatalProblemsNameEachMissingKey(t *testing.T) {
	setEnvs(t, nil)
	for _, k := range []string{"PRODUCT_INTERNAL_TOKEN", "PLATFORM_IDENTITY_BASE_URL", "PLATFORM_IDENTITY_APP_ID", "PLATFORM_IDENTITY_TOKEN"} {
		c := Load()
		found := false
		for _, p := range c.FatalProblems() {
			if strings.Contains(p, k) {
				found = true
			}
		}
		if !found {
			t.Fatalf("缺少 %s 时致命问题未点名该键: %v", k, c.FatalProblems())
		}
		t.Setenv(k, "x")
	}
}

func TestGenerationGateMatrix(t *testing.T) {
	// off → 明确中文原因
	setEnvs(t, fullEnv())
	c := Load()
	if st, msg := c.GenerationUsable(); st != 503 || !strings.Contains(msg, "FEATURE_GENERATION_ENABLED") {
		t.Fatalf("开关 off 应 503 且点名开关, got %d %q", st, msg)
	}
	// on 但缺 task 配置 → 点名缺失键
	t.Setenv("FEATURE_GENERATION_ENABLED", "1")
	t.Setenv("PLATFORM_TASK_BASE_URL", "")
	c = Load()
	if st, msg := c.GenerationUsable(); st != 503 || !strings.Contains(msg, "PLATFORM_TASK_BASE_URL") {
		t.Fatalf("缺 task 配置应 503 点名键, got %d %q", st, msg)
	}
	// on 且配置齐 → 放行
	t.Setenv("PLATFORM_TASK_BASE_URL", "http://127.0.0.1:18103")
	c = Load()
	if st, msg := c.GenerationUsable(); st != 0 {
		t.Fatalf("配置齐应放行, got %d %q", st, msg)
	}
}

func TestBillingGateMatrix(t *testing.T) {
	setEnvs(t, fullEnv())
	c := Load()
	if st, msg := c.BillingUsable(); st != 503 || !strings.Contains(msg, "ECO_BILLING_ENABLED") {
		t.Fatalf("计费开关 off 应 503 且点名开关, got %d %q", st, msg)
	}
	t.Setenv("ECO_BILLING_ENABLED", "1")
	t.Setenv("PLATFORM_BILLING_TOKEN", "")
	c = Load()
	if st, msg := c.BillingUsable(); st != 503 || !strings.Contains(msg, "PLATFORM_BILLING_TOKEN") {
		t.Fatalf("缺 billing 配置应 503 点名键, got %d %q", st, msg)
	}
	t.Setenv("PLATFORM_BILLING_TOKEN", "billtok")
	c = Load()
	if st, _ := c.BillingUsable(); st != 0 {
		t.Fatal("配置齐应放行")
	}
}

func TestFullConfigHasNoFatalProblems(t *testing.T) {
	setEnvs(t, fullEnv())
	if p := Load().FatalProblems(); len(p) != 0 {
		t.Fatalf("全配置不应有致命问题: %v", p)
	}
}

// D-A1:对端内部令牌只进回执投递头;缺省必须为空(空=不发 Authorization)。
func TestOrderInternalTokenEnv(t *testing.T) {
	setEnvs(t, nil)
	if tok := Load().OrderInternalToken; tok != "" {
		t.Fatalf("缺省必须为空(不发认证头), got %q", tok)
	}
	setEnvs(t, map[string]string{"PRODUCT_ORDER_INTERNAL_TOKEN": "peer-tok"})
	if tok := Load().OrderInternalToken; tok != "peer-tok" {
		t.Fatalf("PRODUCT_ORDER_INTERNAL_TOKEN 应读入, got %q", tok)
	}
}
