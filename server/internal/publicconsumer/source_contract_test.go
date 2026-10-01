package publicconsumer

import (
	"reflect"
	"testing"
	"time"

	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

// Independently frozen consumer expectations: catalogue presence alone cannot
// establish method, authority, quote guard, retry query, or scope semantics.
func TestSourcePublishedContractMetadata(t *testing.T) {
	rows := []struct {
		id, service, method, path, idem, retry, query, tenant, payer string
		ms                                                           int
		mutating, queryApp                                           bool
	}{
		{"identity.context.resolve", "identity", "POST", "/internal/v1/identity/context", "none", "transport_only", "", "client_hint_server_verified", "server_billing_segment", 3000, false, false},
		{"billing.usage", "billing", "POST", "/internal/v1/billing/unified/usage", "body", "query_before_retry", "billing.usage_lookup", "not_a_request_field", "usage_fact", 5000, true, false},
		{"billing.usage_lookup", "billing", "GET", "/internal/v1/billing/unified/usage/by-idempotency", "none", "transport_only", "", "not_a_request_field", "usage_fact", 5000, false, true},
		{"billing.usage_get", "billing", "GET", "/internal/v1/billing/unified/usage/{id}", "none", "transport_only", "", "not_a_request_field", "usage_fact", 5000, false, true},
		{"billing.source_quote", "billing", "POST", "/internal/v1/billing/unified/quote", "none", "query_before_retry", "billing.usage_get", "not_a_request_field", "usage_fact", 5000, true, false},
		{"task.source_submit", "task", "POST", "/internal/v1/tasks", "body", "query_before_retry", "task.source_lookup", "not_a_request_field", "body_payer_rechecked", 10000, true, false},
		{"task.source_get", "task", "GET", "/internal/v1/tasks/{id}", "none", "transport_only", "", "not_a_request_field", "query_payer_rechecked", 3000, false, true},
		{"task.source_lookup", "task", "GET", "/internal/v1/tasks/by-idempotency", "none", "transport_only", "", "not_a_request_field", "query_payer_rechecked", 3000, false, true},
		{"task.source_cancel", "task", "POST", "/internal/v1/tasks/{id}/cancel", "none", "query_before_retry", "task.source_get", "not_a_request_field", "query_payer_rechecked", 5000, true, true},
		{"task.source_reconcile", "task", "POST", "/internal/v1/tasks/{id}/reconcile", "none", "query_before_retry", "task.source_get", "not_a_request_field", "query_payer_rechecked", 5000, true, true},
		{"upload.durable_asset_get", "upload", "GET", "/internal/v1/upload/durable/assets/{asset_id}", "none", "transport_only", "", "server_authenticated_user", "not_a_request_field", 10000, false, true},
		{"upload.durable_download", "upload", "POST", "/internal/v1/upload/durable/assets/{asset_id}/download-url", "none", "query_before_retry", "upload.durable_asset_get", "server_authenticated_user", "not_a_request_field", 10000, false, false},
		{"upload.durable_reference_get", "upload", "GET", "/internal/v1/upload/durable/assets/{asset_id}/references/{reference_id}", "none", "transport_only", "", "server_authenticated_user", "not_a_request_field", 10000, false, true},
		{"upload.durable_release", "upload", "POST", "/internal/v1/upload/durable/assets/{asset_id}/references/{reference_id}/release", "none", "query_before_retry", "upload.durable_reference_get", "server_authenticated_user", "not_a_request_field", 10000, true, false},
	}
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			op, ok, e := pc.Lookup(r.id)
			if e != nil || !ok {
				t.Fatal("missing published operation", e)
			}
			auth := []string{"app"}
			if r.service == "billing" || r.service == "identity" {
				auth = append(auth, "service")
			}
			bodyApp := ""
			if r.method == "POST" && !r.queryApp {
				bodyApp = "app_id"
			}
			if op.Service != r.service || op.Method != r.method || op.Path != r.path || op.Idempotency != r.idem || op.Retry != r.retry || op.QueryOperation != r.query || op.TenantSource != r.tenant || op.PayerSource != r.payer || op.BrandSource != "not_a_request_field" || op.Timeout != time.Duration(r.ms)*time.Millisecond || op.Mutating != r.mutating || op.QueryApp != r.queryApp || op.BodyAppField != bodyApp || op.Streaming || op.QuoteGuard || !reflect.DeepEqual(op.Auth, auth) {
				t.Fatalf("published consumer contract drift: %+v", op)
			}
		})
	}
}
