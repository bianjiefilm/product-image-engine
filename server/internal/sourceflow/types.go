package sourceflow

import (
	"context"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	"io"
	"strings"
	"sync/atomic"
	"time"
)

type Actor struct{ UserID, AccountID string }
type Authorization struct {
	Scope             si.Scope
	PayerSource, Role string
}
type ContextPort interface {
	Resolve(context.Context, si.ContextQuery) (si.ContextFact, error)
}
type BillPort interface {
	Lookup(context.Context, si.Run) (si.BillFact, bool, error)
	Get(context.Context, si.Run) (si.BillFact, bool, error)
	Usage(context.Context, si.Run) error
	Quote(context.Context, si.Run) error
}
type TaskPort interface {
	Lookup(context.Context, si.Run) (si.TaskFact, bool, error)
	Get(context.Context, si.Run) (si.TaskFact, bool, error)
	Submit(context.Context, si.Run) error
	Cancel(context.Context, si.Run) error
	Reconcile(context.Context, si.Run) error
}
type AssetPort interface {
	Verify(context.Context, si.Run) (si.Output, error)
	Reference(context.Context, si.Outbox) (string, error)
	Release(context.Context, si.Outbox) error
	Download(context.Context, si.Run) (io.ReadCloser, error)
}
type Service struct {
	recoveryRunning     atomic.Bool
	OutputRecoveryReady bool // Set only by the actual joined runtime assembly; default false.
	Store               *store.Store
	Auth                *Authorizer
	Bill                BillPort
	Tasks               TaskPort
	Assets              AssetPort
	Profile             Profile
	Now                 func() time.Time
}
type Profile struct {
	Enabled, OrgEnabled                               bool
	Provider, Model, Capability, Size, PricingVersion string
	Quantity                                          int64
}

func (p Profile) CanCreate(source string) bool {
	return p.Enabled && p.PricingVersion != "" && strings.TrimSpace(p.PricingVersion) == p.PricingVersion && len(p.PricingVersion) <= 256 && p.Provider == "modelxing-qwen-image-2.0-v1" && p.Model == "qwen-image-2.0" && p.Capability == "image.generate" && p.Size == "1024*1024" && p.Quantity == 1 && (source == "personal" || source == "delegation" && p.OrgEnabled)
}
func NewProfile(c config.Config) Profile {
	p := Profile{Enabled: c.SourceImagesEnabled && c.GenerationEnabled && c.BillingEnabled, OrgEnabled: c.SourceOrgImagesEnabled, Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Quantity: 1, PricingVersion: c.SourcePricingVersion}
	if c.AppID == "" || c.AppID != c.IdentityAppID || c.SourceDownloadHosts == "" {
		p.Enabled = false
	}
	for _, base := range []string{c.IdentityBaseURL, c.BillingBaseURL, c.TaskBaseURL, c.UploadBaseURL} {
		if base == "" {
			p.Enabled = false
		}
	}
	seen := map[string]bool{}
	for _, token := range []string{c.IdentityToken, c.BillingToken, c.TaskToken, c.UploadToken} {
		if token == "" || seen[token] {
			p.Enabled = false
		}
		seen[token] = true
	}
	return p
}

// CreationCapabilities separates quote/confirmation from complete product
// readiness. Merely injecting ports cannot grant the joined output lifecycle.
func (s *Service) CreationCapabilities(payerSource string) (quote, confirm, ready bool) {
	if s == nil || s.Store == nil || s.Auth == nil || !s.Profile.CanCreate(payerSource) {
		return
	}
	quote = !nilPort(s.Bill)
	confirm = quote && !nilPort(s.Tasks)
	ready = confirm && !nilPort(s.Assets) && s.OutputRecoveryReady
	return
}
