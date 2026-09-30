package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageTaskUsesCanonicalPlatformContract(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/internal/v1/tasks" || r.Method != "POST" {
			t.Errorf("wrong operation: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["app_id"] != "product-image" || body["account_id"] != "acct_verified" || body["capability"] != "image.generate" || body["idempotency_key"] != "frozen-key" {
			t.Errorf("wrong authority/request: %#v", body)
		}
		if body["kind"] != nil || body["payload"] != nil || body["token"] != nil {
			t.Errorf("legacy or secret body: %#v", body)
		}
		if r.Header.Get(internalTokenHeader) != "test-only" {
			t.Error("missing app credential")
		}
		billing := body["billing"].(map[string]any)
		if billing["mode"] != "hold" || billing["amount_cny"] != 1.23 {
			t.Errorf("funds must be platform task owned: %#v", billing)
		}
		io.WriteString(w, `{"task_id":"tsk_one","status":"QUEUED","hold_id":"hold_one"}`)
	}))
	defer srv.Close()
	c := &TaskClient{BaseURL: srv.URL, AppID: "product-image", Token: "test-only"}
	in := ImageTaskRequest{AccountID: "acct_verified", ProjectID: "prj_one", IdempotencyKey: "frozen-key", Provider: "xingmo", Params: json.RawMessage(`{"prompt":"empty studio","model":"verified-model","size":"1024x1024"}`), AmountMinor: 123}
	got, err := c.SubmitImage(context.Background(), in)
	if err != nil || got.TaskID != "tsk_one" || got.Status != "queued" || got.HoldID != "hold_one" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	in.AccountID = ""
	if _, err = c.SubmitImage(context.Background(), in); err == nil || calls != 1 {
		t.Fatal("invalid payer reached platform")
	}
}

func TestImageTaskPollingDecodesPlatformResultAndRejectsWrongID(t *testing.T) {
	wrong := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app_id") != "product-image" {
			t.Error("missing app scope")
		}
		id := "tsk_one"
		if wrong {
			id = "tsk_other"
		}
		json.NewEncoder(w).Encode(map[string]any{"task_id": id, "status": "SUCCEEDED", "hold_id": "hold_one", "result_json": `{"asset_id":"asset_one","sha256":"hash"}`})
	}))
	defer srv.Close()
	c := &TaskClient{BaseURL: srv.URL, AppID: "product-image", Token: "test-only"}
	got, err := c.Status(context.Background(), "tsk_one")
	if err != nil || got.Status != "succeeded" || got.Result["asset_id"] != "asset_one" || got.HoldID != "hold_one" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	wrong = true
	if _, err = c.Status(context.Background(), "tsk_one"); err == nil {
		t.Fatal("another task cannot be adopted")
	}
}

func TestImageTaskErrorsNeverExposeProviderPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-provider-error", 502) }))
	defer srv.Close()
	c := &TaskClient{BaseURL: srv.URL, AppID: "product-image", Token: "test-only"}
	_, err := c.SubmitImage(context.Background(), ImageTaskRequest{AccountID: "a", ProjectID: "p", IdempotencyKey: "i", Provider: "xingmo", Params: json.RawMessage(`{"model":"m"}`), AmountMinor: 100})
	if err == nil || strings.Contains(err.Error(), "secret-provider-error") {
		t.Fatalf("unsafe error: %v", err)
	}
}
