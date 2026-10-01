package publicconsumer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

func TestLoopbackSessionResolveSendsAppHeaders(t *testing.T) {
	var gotApp, gotToken, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotApp = r.Header.Get(platformconsumer.HeaderApp)
		gotToken = r.Header.Get(platformconsumer.HeaderToken)
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":"unauthorized"}`)
	}))
	t.Cleanup(srv.Close)

	client := &platformconsumer.Client{
		Caller: platformconsumer.Caller{Mode: "app", AppID: "product-image", Token: "tok-test"},
		Bases:  platformconsumer.Bases{Identity: srv.URL},
	}
	result, err := client.Call(context.Background(), platformconsumer.Call{
		Operation: "identity.session.resolve",
	})
	if !result.Sent {
		t.Fatalf("request was not sent: err=%v class=%s", err, result.Classification)
	}
	if gotApp != "product-image" || gotToken != "tok-test" {
		t.Fatalf("headers app=%q token=%q", gotApp, gotToken)
	}
	if gotPath != "/internal/v1/identity/session/resolve" {
		t.Fatalf("path %s", gotPath)
	}
	if result.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", result.StatusCode)
	}
	if result.Classification.Status != "unauthenticated" || result.Classification.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("classification %+v", result.Classification)
	}
}

func TestSourceSDKOperationsPublished(t *testing.T) {
	for _, id := range []string{"identity.context.resolve", "billing.usage", "billing.usage_lookup", "billing.usage_get", "billing.source_quote", "task.source_submit", "task.source_get", "task.source_lookup", "task.source_cancel", "task.source_reconcile", "upload.durable_asset_get", "upload.durable_download", "upload.durable_reference_get", "upload.durable_release"} {
		if _, ok, err := platformconsumer.Lookup(id); err != nil || !ok {
			t.Errorf("required published operation %s missing: %v", id, err)
		}
	}
}
