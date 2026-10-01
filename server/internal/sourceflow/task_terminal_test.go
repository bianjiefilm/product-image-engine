package sourceflow

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

// Reproduces independent terminal review cases against published SDK HTTP.
func TestSourceTaskTerminalCancellationPreservesFacts(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "canceled"} {
		for _, when := range []string{"known-terminal", "terminal-after-intent", "terminal-read-error"} {
			t.Run(status+"/"+when, func(t *testing.T) {
				f := flowFixture(t)
				r := f.confirmed(t)
				billGets := f.bill.Gets
				billWrites := f.bill.UsageAttempts + f.bill.QuoteAttempts
				var mu sync.Mutex
				current := status
				cancels := 0
				unavailable := false
				if when == "terminal-after-intent" {
					current = "queued"
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if req.Method == http.MethodPost {
						cancels++
						t.Error("Task cancellation must not be sent for known terminal")
						w.WriteHeader(503)
						return
					}
					if req.Header.Get("X-App-ID") != r.Intent.Scope.AppID || req.Header.Get("X-PilotSeaview-Internal-Token") != "review-task" {
						t.Error("wrong authority")
					}
					if unavailable {
						w.WriteHeader(503)
						return
					}
					body := map[string]any{"task_id": "terminal-original", "funding_version": "source_reservation_v1", "app_id": r.Intent.Scope.AppID, "payer_account_id": r.Intent.Scope.PayerAccountID, "payer_user_id": r.Intent.Scope.UserID, "project_id": r.Intent.Scope.ProjectID, "idempotency_key": r.TaskKey, "capability": r.Intent.Capability, "provider": r.Intent.Provider, "status": current, "phase": current, "usage_id": r.Quote.UsageID, "quote_id": r.Quote.QuoteID, "quantity": r.Quote.Quantity, "unit_price_minor": r.Quote.UnitPriceMinor, "amount_minor": r.Quote.AmountMinor, "pricing_version": r.Quote.PricingVersion, "business_ref": r.BusinessRef, "hold_id": "", "charge_id": "", "release_id": "", "provider_result_received": false, "output_ready": false, "result_json": "", "error_code": "", "next_retry_at": "", "created_at": "2026-10-01T00:00:00Z"}
					if current == "succeeded" {
						body["hold_id"] = "hold-original"
						body["charge_id"] = "charge-original"
						body["provider_result_received"] = true
						body["output_ready"] = true
						raw, _ := json.Marshal(map[string]any{"asset_id": "asset-original", "reference_id": "ref-original", "project_id": r.Intent.Scope.ProjectID, "content_type": "image/png", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "size_bytes": int64(123)})
						body["result_json"] = string(raw)
					}
					if current == "failed" || current == "canceled" {
						body["hold_id"] = "hold-original"
						body["release_id"] = "release-original"
					}
					json.NewEncoder(w).Encode(body)
				}))
				defer upstream.Close()
				clients, err := platform.NewSourceClients(config.Config{AppID: r.Intent.Scope.AppID, IdentityAppID: r.Intent.Scope.AppID, IdentityBaseURL: upstream.URL, BillingBaseURL: upstream.URL, TaskBaseURL: upstream.URL, UploadBaseURL: upstream.URL, IdentityToken: "review-identity", BillingToken: "review-bill", TaskToken: "review-task", UploadToken: "review-upload"}, upstream.Client())
				if err != nil {
					t.Fatal(err)
				}
				f.svc.Tasks = clients.Tasks
				before, err := f.svc.Advance(t.Context(), r.ID)
				if err != nil || before.Task == nil {
					t.Fatal("valid terminal fixture rejected", before, err)
				}
				if when == "known-terminal" {
					_, err = f.svc.Cancel(t.Context(), f.actor, f.project.ID, r.ID)
					if err != nil && !errors.Is(err, si.ErrConflict) {
						t.Fatal("unexpected rejection", err)
					}
					after, loadErr := f.store.GetSourceRun(t.Context(), r.Intent.Scope, r.ID)
					if loadErr != nil || !reflect.DeepEqual(before, after) {
						t.Fatalf("P2 known %s cancel rewrote original facts: phase %s→%s cancel %v→%v revision %d→%d err=%v", status, before.Phase, after.Phase, before.CancelRequested, after.CancelRequested, before.Revision, after.Revision, loadErr)
					}
				} else if when == "terminal-read-error" {
					// Represent a durable historical cancel intent with already observed
					// terminal facts; do not invoke a financial or Task writer to prepare it.
					db, err := sql.Open("sqlite", f.path)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = db.ExecContext(t.Context(), "UPDATE product_source_runs SET cancel_requested=1 WHERE id=?", r.ID); err != nil {
						t.Fatal(err)
					}
					db.Close()
					mu.Lock()
					unavailable = true
					mu.Unlock()
					f.reopen(t)
					f.svc.Tasks = clients.Tasks
					after, err := f.svc.Advance(t.Context(), r.ID)
					if err == nil || after.Phase != before.Phase || !reflect.DeepEqual(after.Task, before.Task) || !after.CancelRequested || after.Output != nil || !reflect.DeepEqual(after.Bill, before.Bill) {
						t.Fatalf("P2 error retry covered known terminal %s: phase=%s expected=%s err=%v", status, after.Phase, before.Phase, err)
					}
				} else {
					intent, err := f.store.CancelSourceRun(t.Context(), r.Intent.Scope, r.ID)
					if err != nil || !intent.CancelRequested {
						t.Fatal(intent, err)
					}
					mu.Lock()
					current = status
					mu.Unlock()
					f.reopen(t)
					f.svc.Tasks = clients.Tasks
					after, err := f.svc.Advance(t.Context(), r.ID)
					expected := "review_required"
					if status == "succeeded" {
						expected = "asset_pending"
					}
					if err != nil || after.Task == nil || after.Task.Status != status || after.Phase != expected || !after.CancelRequested || after.Output != nil || !reflect.DeepEqual(after.Bill, before.Bill) {
						t.Fatalf("P2 original canceled intent covered known terminal %s: phase=%s expected=%s err=%v", status, after.Phase, expected, err)
					}
				}
				if f.bill.Gets != billGets || f.bill.UsageAttempts+f.bill.QuoteAttempts != billWrites {
					t.Fatal("terminal cancellation touched Bill authority")
				}
				mu.Lock()
				defer mu.Unlock()
				if cancels != 0 {
					t.Fatal("unexpected Task mutation", cancels)
				}
			})
		}
	}
}

func TestSourceTaskErrorPreservesLocalTerminalWithNullTask(t *testing.T) {
	for _, phase := range []string{"succeeded", "failed", "canceled", "not_dispatched"} {
		t.Run(phase, func(t *testing.T) {
			f := flowFixture(t)
			r := f.confirmed(t)
			db, e := sql.Open("sqlite", f.path)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec(`UPDATE product_source_runs SET phase=?,cancel_requested=1,submit_attempted=1 WHERE id=?`, phase, r.ID); e != nil {
				t.Fatal(e)
			}
			db.Close()
			r, claimed, e := f.store.ClaimSourceRun(t.Context(), r.Intent.Scope, r.ID, f.now.Unix(), 30)
			if e != nil || !claimed || r.Task != nil {
				t.Fatal(r, e)
			}
			out, e := f.svc.taskError(t.Context(), r, si.ErrUnavailable)
			if !errors.Is(e, si.ErrUnavailable) || out.Phase != phase || !out.CancelRequested || out.Task != nil || out.Output != nil || out.TaskID != "" {
				t.Fatal("error projected local terminal to pending", out, e)
			}
			if f.task.Cancels != 0 || f.task.Submits != 0 {
				t.Fatal("terminal Task mutation")
			}
		})
	}
}
