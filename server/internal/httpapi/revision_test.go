package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
)

func TestRevisionRoutesHiddenWhenDisabled(t *testing.T) {
	f := newFixture(t, nil)
	_, pair := f.login(t, "a@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, _ := f.do(t, "POST", "/api/v1/projects/proj_missing/revisions/intent", tok, map[string]any{"text": "背景简单一点"})
	if st != http.StatusNotFound {
		t.Fatalf("disabled route status = %d", st)
	}
}

func TestSelectiveRevisionFlow(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "upload must not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	f := newFixture(t, func(c *config.Config) { c.SelectiveRevisionEnabled = true })
	f.srv.Uploads.BaseURL = upstream.URL
	f.srv.Tasks.BaseURL = upstream.URL
	_, pair := f.login(t, "rev@x.com", "right-pass")
	tok, _ := pair["access_token"].(string)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "只改这里", "source_type": "standalone"})
	if st != http.StatusCreated {
		t.Fatalf("create project %d %v", st, created)
	}
	proj := created["project"].(map[string]any)["id"].(string)
	base := "/api/v1/projects/" + proj + "/revisions"

	st, byText := f.do(t, "POST", base+"/intent", tok, map[string]any{"text": "背景简单一点"})
	stClick, byClick := f.do(t, "POST", base+"/intent", tok, map[string]any{"click": "simplify_background"})
	if st != http.StatusOK || stClick != http.StatusOK {
		t.Fatalf("intent status %d %d", st, stClick)
	}
	if byText["incremental_cost_cents"] != float64(40) || byClick["incremental_cost_cents"] != float64(40) {
		t.Fatalf("cost text=%v click=%v", byText["incremental_cost_cents"], byClick["incremental_cost_cents"])
	}
	textIntent := byText["intent"].(map[string]any)
	clickIntent := byClick["intent"].(map[string]any)
	if textIntent["action"] != clickIntent["action"] || textIntent["preserve_subject"] != true || textIntent["region"] != clickIntent["region"] {
		t.Fatalf("intent diverged %#v %#v", textIntent, clickIntent)
	}

	st, plan := f.do(t, "POST", base+"/plan", tok, map[string]any{"text": "换成户外"})
	if st != http.StatusOK {
		t.Fatalf("plan %d %v", st, plan)
	}
	body := plan["plan"].(map[string]any)
	if body["requires_acknowledgement"] != true || body["impact_notice"] == "" || body["calls_supplier"] != false {
		t.Fatalf("plan = %#v", body)
	}
	if hits != 0 {
		t.Fatal("planning called upstream")
	}

	st, denied := f.do(t, "POST", base, tok, map[string]any{"text": "背景简单一点", "acknowledged": false})
	if st != http.StatusConflict || errCode(denied) != "impact_unacknowledged" {
		t.Fatalf("unacked %d %#v", st, denied)
	}
	st, listed := f.do(t, "GET", base, tok, nil)
	if st != http.StatusOK || len(listed["versions"].([]any)) != 0 {
		t.Fatalf("unacked wrote a version: %d %#v", st, listed)
	}

	original, mask := revisionFixture(t)
	st, made := f.do(t, "POST", base, tok, map[string]any{
		"text": "背景简单一点", "acknowledged": true, "brief_version": "b1",
		"original_b64": b64(original), "mask_b64": b64(mask),
	})
	if st != http.StatusCreated {
		t.Fatalf("execute %d %#v", st, made)
	}
	if made["supplier_calls"] != float64(0) || made["origin"] != "subject_lock" || made["subject_preserved"] != true {
		t.Fatalf("execute body %#v", made)
	}
	if made["incremental_cost_cents"] != float64(40) {
		t.Fatalf("stored cost %#v", made["incremental_cost_cents"])
	}
	ran, _ := made["steps_run"].([]any)
	for _, step := range ran {
		for _, banned := range revision.ExpensiveSteps {
			if step == banned {
				t.Fatalf("expensive step %v", step)
			}
		}
	}
	v1 := made["version"].(map[string]any)["label"].(string)
	code, hdr, raw := f.doRaw(t, "GET", base+"/"+v1+"/content", tok, nil)
	if code != http.StatusOK || hdr.Get("X-Revision-Version") != v1 {
		t.Fatalf("content %d %s", code, hdr.Get("X-Revision-Version"))
	}
	assertHTTPSubject(t, original, mask, raw)

	st, second := f.do(t, "POST", base, tok, map[string]any{
		"click": "outdoor", "acknowledged": true, "brief_version": "b1",
		"original_b64": b64(original), "mask_b64": b64(mask),
	})
	if st != http.StatusCreated {
		t.Fatalf("second %d %#v", st, second)
	}
	v2 := second["version"].(map[string]any)["label"].(string)
	st, adopted := f.do(t, "POST", base+"/"+v2+"/adopt", tok, map[string]any{})
	if st != http.StatusOK || adopted["adopted_label"] != v2 {
		t.Fatalf("adopt %d %#v", st, adopted)
	}
	adoptedHash, _ := adopted["adopted_content_hash"].(string)

	st, failed := f.do(t, "POST", base+"/outcome", tok, map[string]any{"status": "failed", "text": "背景简单一点"})
	if st != http.StatusOK || failed["adopted_label"] != v2 || failed["adopted_content_hash"] != adoptedHash {
		t.Fatalf("failed overwrote adopted: %d %#v", st, failed)
	}
	st, unknown := f.do(t, "POST", base+"/outcome", tok, map[string]any{"status": "unknown", "click": "outdoor"})
	if st != http.StatusOK || unknown["adopted_content_hash"] != adoptedHash {
		t.Fatalf("unknown overwrote adopted: %#v", unknown)
	}

	st, _ = f.do(t, "POST", base+"/briefs", tok, map[string]any{"version": "b1", "facts": map[string]string{"place": "studio"}})
	if st != http.StatusOK {
		t.Fatalf("seed brief %d", st)
	}
	st, brief := f.do(t, "POST", base+"/briefs", tok, map[string]any{"version": "b2", "facts": map[string]string{"place": "outdoor"}})
	if st != http.StatusOK {
		t.Fatalf("brief %d %#v", st, brief)
	}
	diff := brief["diff"].(map[string]any)
	if diff["adopted_unchanged"] != true || diff["adopted_content_hash"] != adoptedHash {
		t.Fatalf("brief diff %#v", diff)
	}

	st, pairView := f.do(t, "GET", base+"/compare?left="+v1+"&right="+v2, tok, nil)
	if st != http.StatusOK {
		t.Fatalf("compare %d %#v", st, pairView)
	}
	left := pairView["comparison"].(map[string]any)["left"].(map[string]any)
	right := pairView["comparison"].(map[string]any)["right"].(map[string]any)
	if left["label"] != v1 || right["label"] != v2 || left["content_hash"] == right["content_hash"] {
		t.Fatalf("compare %#v", pairView["comparison"])
	}

	st, back := f.do(t, "POST", base+"/rollback", tok, map[string]any{"text": "回到" + v1})
	if st != http.StatusOK || back["adopted_label"] != v1 {
		t.Fatalf("rollback %d %#v", st, back)
	}
	st, exported := f.do(t, "POST", base+"/"+v2+"/export", tok, map[string]any{})
	if st != http.StatusOK {
		t.Fatalf("export %d %#v", st, exported)
	}
	exp := exported["export"].(map[string]any)
	if exp["version_id"] != v2 || exp["requires_reupload"] != false || exp["asset_ref"] == "" {
		t.Fatalf("export %#v", exp)
	}
	st, down := f.do(t, "POST", base+"/"+v2+"/downstream", tok, map[string]any{"target": "matrix"})
	if st != http.StatusOK {
		t.Fatalf("downstream %d %#v", st, down)
	}
	ref := down["downstream"].(map[string]any)
	if ref["version_id"] != v2 || ref["requires_reupload"] != false || ref["content_hash"] != exp["content_hash"] {
		t.Fatalf("downstream %#v", ref)
	}
	if hits != 0 {
		t.Fatalf("upstream calls = %d", hits)
	}

	_, other := f.login(t, "other@x.com", "right-pass")
	otherTok, _ := other["access_token"].(string)
	st, _ = f.do(t, "GET", base, otherTok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("cross tenant %d", st)
	}
}

func revisionFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	mask := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x >= 2 && x < 6 && y >= 2 && y < 6 {
				img.SetNRGBA(x, y, color.NRGBA{180, 20, 20, 255})
				mask.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{10, 10, 10, 255})
				mask.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 0})
			}
		}
	}
	return encodeHTTPPNG(t, img), encodeHTTPPNG(t, mask)
}

func encodeHTTPPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertHTTPSubject(t *testing.T, original, mask, result []byte) {
	t.Helper()
	src, err := png.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	matte, err := png.Decode(bytes.NewReader(mask))
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			_, _, _, a := matte.At(x, y).RGBA()
			if a>>8 < 128 {
				continue
			}
			if src.At(x, y) != got.At(x, y) {
				t.Fatalf("subject pixel changed at %d,%d", x, y)
			}
		}
	}
}
