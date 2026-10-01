package sourceimage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceQualityNeverClaimsFidelityOrHumanAdoption(t *testing.T) {
	r := Run{ID: "run-quality", Owner: Owner, Intent: Intent{Scope: Scope{AppID: "product-image", TenantID: "acct-a", ProjectID: "project-a", UserID: "user-a", PrincipalAccountID: "acct-a", PayerAccountID: "acct-a"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "box", PricingVersion: "price-v1", Quantity: 1}, Phase: "asset_pending"}
	raw, e := QualityReport(r)
	if e != nil {
		t.Fatal(e)
	}
	var report map[string]any
	if e = json.Unmarshal(raw, &report); e != nil {
		t.Fatal(e)
	}
	if report["report_version"] != "product-source-quality/v1" || report["fidelity"] != "not_applicable" || report["visual_quality"] != "unknown" || report["human_adoption"] != "NOT_RUN" || strings.Contains(string(raw), "\"pass\":true") {
		t.Fatal(string(raw))
	}
	for _, edit := range []func(*Run){func(r *Run) { r.Output = &Output{AssetID: "unverified"} }, func(r *Run) { r.ChargedBill = &BillFact{ChargeID: "fake"} }, func(r *Run) { r.TaskID = "unbound" }, func(r *Run) { r.Bill = &BillFact{Status: "charged"} }} {
		bad := r
		edit(&bad)
		if _, e = QualityReport(bad); e == nil {
			t.Fatal("report invented authority", bad)
		}
	}
	r.Intent.Mode = "reference_edit"
	if _, e = QualityReport(r); e == nil {
		t.Fatal("ordinary report accepted unimplemented edit")
	}
}
