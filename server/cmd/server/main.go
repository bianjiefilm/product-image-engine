// product-image-server — 产品图独立应用底座(HUI-1739 I0)。
//
// 启动:配置全部来自进程 env(样例见 deploy/product-image.env.example,值全占位)。
// 致命配置缺失不退出:进程保持可观测,/readyz 报 503+原因,受保护端点一律 fail-closed。
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/httpapi"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
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

	s := &httpapi.Server{
		Cfg: cfg, St: st,
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
