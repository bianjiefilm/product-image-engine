package sourceimage

import (
	"strings"
	"testing"
)

func TestSourceTaskOriginalIDsAndTerminalFactsAreImmutable(t *testing.T) {
	in := Intent{Scope: Scope{AppID: "product-image", TenantID: "acct-a", ProjectID: "project-a", UserID: "user-a", PrincipalAccountID: "acct-a", PayerAccountID: "acct-a"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "box", PricingVersion: "price-v1", Quantity: 1}
	r := Run{ID: "run-a", Owner: Owner, Intent: in, TaskKey: "task-key", Quote: &Quote{UsageID: "usage-a", Quantity: 1, AmountMinor: 25}}
	fact := TaskFact{ID: "task-a", IdempotencyKey: r.TaskKey, Scope: in.Scope, Quote: *r.Quote, Provider: in.Provider, Capability: in.Capability, Status: "succeeded", Phase: "succeeded", HoldID: "hold-a", ChargeID: "charge-a", Output: &Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: in.Scope.ProjectID, AppID: in.Scope.AppID, PrincipalType: "user", PrincipalID: in.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 100}}
	r.TaskID = fact.ID
	r.Task = &fact
	if e := ValidateTaskFact(r, fact); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*TaskFact){func(f *TaskFact) { f.HoldID = "hold-b" }, func(f *TaskFact) { f.ChargeID = "charge-b" }, func(f *TaskFact) { f.ReleaseID = "release-b" }, func(f *TaskFact) { f.ID = "task-b" }, func(f *TaskFact) { f.Status = "running" }, func(f *TaskFact) { f.Output = nil }, func(f *TaskFact) { out := *f.Output; out.ReferenceID = "ref-b"; f.Output = &out }} {
		wrong := fact
		mutate(&wrong)
		if e := ValidateTaskFact(r, wrong); e == nil {
			t.Fatal("immutable Task fact accepted", wrong)
		}
	}
}
