// product-image-server — 产品图独立应用底座(HUI-1739 I0)。
//
// 启动:配置全部来自进程 env(样例见 deploy/product-image.env.example,值全占位)。
// 致命配置缺失不退出:进程保持可观测,/readyz 报 503+原因,受保护端点一律 fail-closed。
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/httpapi"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

func main() {
	cfg := config.Load()
	// 启动横幅绝不打印任何 token 值。
	log.Printf("product-image-server: addr=%s db=%s app=%s billing_enabled=%v generation_enabled=%v",
		cfg.Addr, cfg.DBPath, cfg.IdentityAppID, cfg.BillingEnabled, cfg.GenerationEnabled)

	if fatal := cfg.FatalProblems(); len(fatal) > 0 {
		log.Printf("product-image-server: 配置不完整(受保护端点将 fail-closed): %s", strings.Join(fatal, "; "))
	}

	// 数据目录(库文件所在)按需创建;不回退到任何共享 SQLite。
	if dir := filepath.Dir(cfg.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("product-image-server: 创建数据目录失败: %v", err)
		}
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("product-image-server: 打开数据库失败: %v", err)
	}
	defer st.Close()

	// 应用登记表(HUI-1745 I1):优先 PRODUCT_REGISTRY_MANIFEST 文件;
	// 缺省内嵌 PROVISIONAL 样例。加载即校验,失败拒绝启动(fail-closed)。
	registry, err := loadRegistry(cfg.RegistryManifestPath)
	if err != nil {
		log.Fatalf("product-image-server: %v", err)
	}

	// 尺寸适配预设集(HUI-1703 FEAT-0204):优先 PRODUCT_SIZE_PRESETS 文件;
	// 缺省内嵌公开常见规格整理。加载即校验,失败拒绝启动(fail-closed)。
	presets, err := loadSizePresets(cfg.SizePresetsPath)
	if err != nil {
		log.Fatalf("product-image-server: %v", err)
	}

	s := &httpapi.Server{
		Cfg: cfg, St: st, Registry: registry, Presets: presets,
		Ident:   &platform.IdentityClient{BaseURL: cfg.IdentityBaseURL, AppID: cfg.IdentityAppID, Token: cfg.IdentityToken},
		Verify:  platform.NewVerifier(cfg.IdentityBaseURL, cfg.IdentityAppID, cfg.IdentityIssuer),
		Tasks:   &platform.TaskClient{BaseURL: cfg.TaskBaseURL, AppID: cfg.IdentityAppID, Token: cfg.TaskToken},
		Uploads: &platform.UploadClient{BaseURL: cfg.UploadBaseURL, AppID: cfg.IdentityAppID, Token: cfg.UploadToken},
		Billing: &platform.BillingClient{BaseURL: cfg.BillingBaseURL, AppID: cfg.IdentityAppID, Token: cfg.BillingToken},
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		log.Fatalf("product-image-server: 监听 %s 失败: %v", cfg.Addr, err)
	}
	log.Printf("product-image-server: listening on %s", ln.Addr())
	if err := (&http.Server{Handler: s.Router()}).Serve(ln); err != nil {
		log.Fatalf("product-image-server: serve: %v", err)
	}
}

// loadRegistry 装配应用登记表:配置了路径则读文件,否则内嵌缺省样例。
func loadRegistry(path string) (*appregistry.Manifest, error) {
	if path == "" {
		m, err := appregistry.DefaultManifest()
		if err != nil {
			return nil, fmt.Errorf("内嵌应用登记表校验失败: %w", err)
		}
		return m, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取应用登记表 %s 失败: %w", path, err)
	}
	m, err := appregistry.LoadManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("应用登记表 %s 校验失败: %w", path, err)
	}
	return m, nil
}

// loadSizePresets 装配尺寸适配预设集:配置了路径则读文件,否则内嵌矩阵。
func loadSizePresets(path string) (*sizeadapt.PresetSet, error) {
	if path == "" {
		return sizeadapt.DefaultPresets()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取尺寸适配预设集 %s 失败: %w", path, err)
	}
	s, err := sizeadapt.LoadPresets(raw)
	if err != nil {
		return nil, fmt.Errorf("尺寸适配预设集 %s 校验失败: %w", path, err)
	}
	return s, nil
}
