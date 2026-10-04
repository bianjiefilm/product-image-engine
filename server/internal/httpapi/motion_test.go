package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
)

var motionDigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestMotionRoutesHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, nil)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, _ := f.do(t, "POST", "/api/v1/projects/proj_missing/motion-refs", tok, map[string]any{"revision_id": "V1"})
	if st != http.StatusNotFound {
		t.Fatalf("disabled route status = %d", st)
	}
}

func TestMotionRefRecordsPriceWithoutRegeneration(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "supplier must not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	f := newFixture(t, func(c *config.Config) { c.MotionProductEnabled = true })
	f.srv.Uploads.BaseURL = upstream.URL
	f.srv.Tasks.BaseURL = upstream.URL

	_, pair := f.login(t, "motion@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "动态引用", "source_type": "standalone"})
	if st != http.StatusCreated {
		t.Fatalf("create project %d %v", st, created)
	}
	proj := created["project"].(map[string]any)
	projectID := proj["id"].(string)
	tenantID := proj["tenant_id"].(string)
	base := "/api/v1/projects/" + projectID + "/motion-refs"

	st, missing := f.do(t, "POST", base, tok, map[string]any{"revision_id": "V1"})
	if st != http.StatusConflict || errCode(missing) != "not_adopted" {
		t.Fatalf("unadopted %d %#v", st, missing)
	}
	if !containsNotFinished(missing) {
		t.Fatalf("unadopted copy %#v", missing)
	}

	label, hash := seedAdoptedMotion(t, f, tenantID, projectID)
	st, bad := f.do(t, "POST", base, tok, map[string]any{"revision_id": label, "video_generated": true})
	if st != http.StatusBadRequest {
		t.Fatalf("video field %d %#v", st, bad)
	}

	st, made := f.do(t, "POST", base, tok, map[string]any{"revision_id": label})
	if st != http.StatusCreated {
		t.Fatalf("declare %d %#v", st, made)
	}
	decl := made["declaration"].(map[string]any)
	if decl["asset_id"] == "" || decl["content_hash"] != hash || decl["revision_id"] != label {
		t.Fatalf("declaration identity %#v", decl)
	}
	digest, _ := decl["digest"].(string)
	if !motionDigestRe.MatchString(digest) {
		t.Fatalf("digest = %q", digest)
	}
	if decl["subject_hash"] != hash || decl["product_regeneration"] != false || decl["model_call_count"] != float64(0) {
		t.Fatalf("generation flags %#v", decl)
	}
	if decl["video_generated"] != false || decl["supplier_called"] != false || decl["verified"] != false || decl["dynamic_ad"] != false {
		t.Fatalf("claim flags %#v", decl)
	}
	if !containsNotFinished(decl) {
		t.Fatalf("copy %#v", decl["finished_copy"])
	}
	refID := decl["id"].(string)

	st, again := f.do(t, "POST", base, tok, map[string]any{"revision_id": label})
	if st != http.StatusOK || again["created"] != false {
		t.Fatalf("idempotent %d %#v", st, again)
	}

	st, priced := f.do(t, "POST", base+"/"+refID+"/parameters", tok, map[string]any{"name": "price", "value": "19"})
	if st != http.StatusOK {
		t.Fatalf("price %d %#v", st, priced)
	}
	st, colored := f.do(t, "POST", base+"/"+refID+"/parameters", tok, map[string]any{"name": "color", "value": "暖白"})
	if st != http.StatusOK {
		t.Fatalf("color %d %#v", st, colored)
	}
	after := colored["declaration"].(map[string]any)
	if after["product_regeneration"] != false || after["model_call_count"] != float64(0) {
		t.Fatalf("parameter regeneration %#v", after)
	}
	if after["subject_hash"] != hash || after["content_hash"] != hash || after["digest"] != digest {
		t.Fatalf("subject moved %#v", after)
	}
	if after["dynamic_ad"] != false || after["video_generated"] != false || !containsNotFinished(after) {
		t.Fatalf("parameter claim %#v", after)
	}

	zero := 0
	st, batch := f.do(t, "POST", base+"/batch-rerun", tok, map[string]any{
		"variants": []map[string]any{{"id": "store-b", "execution_count": zero}, {"id": "store-c", "execution_count": zero}},
	})
	if st != http.StatusConflict || errCode(batch) != "batch_rerun_unproven" || !containsNotFinished(batch) {
		t.Fatalf("batch %d %#v", st, batch)
	}
	st, listed := f.do(t, "GET", base, tok, nil)
	if st != http.StatusOK {
		t.Fatalf("list %d %#v", st, listed)
	}
	rows := listed["declarations"].([]any)
	if len(rows) != 1 {
		t.Fatalf("list rows %#v", listed)
	}
	kept := rows[0].(map[string]any)
	if kept["subject_hash"] != hash || kept["model_call_count"] != float64(0) || kept["product_regeneration"] != false {
		t.Fatalf("batch mutated %#v", kept)
	}
	if hits != 0 {
		t.Fatalf("upstream calls = %d", hits)
	}

	_, other := f.login(t, "other-motion@x.com", "right-pass")
	otherTok, _ := other["access_token"].(string)
	st, _ = f.do(t, "GET", base, otherTok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("cross tenant %d", st)
	}
}

func seedAdoptedMotion(t *testing.T, f *fixture, tenantID, projectID string) (string, string) {
	t.Helper()
	led := revision.NewLedger()
	intent, err := revision.ParseNaturalLanguage("背景简单一点")
	if err != nil {
		t.Fatal(err)
	}
	v, err := led.PutCandidate([]byte("png-adopted-motion"), intent, "subject_lock", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Adopt(v.Label); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SaveRevisionLedger(context.Background(), tenantID, projectID, led.Snapshot()); err != nil {
		t.Fatal(err)
	}
	return v.Label, v.ContentHash
}

func containsNotFinished(m map[string]any) bool {
	if copy, ok := m["finished_copy"].(string); ok && strings.Contains(copy, "还没生成成片") {
		return true
	}
	if errBody, ok := m["error"].(map[string]any); ok {
		msg, _ := errBody["message"].(string)
		return strings.Contains(msg, "还没生成成片")
	}
	return false
}
