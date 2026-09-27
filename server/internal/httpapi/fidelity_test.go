package httpapi

import (
	"net/http"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelity"
)

func fidelityBody(checks []map[string]string, extra map[string]any) map[string]any {
	body := map[string]any{
		"mode": "fidelity", "input_ref": "asset-in", "input_version": "v1",
		"output_ref": "", "mask_ref": "mask-1",
		"sample_class":  "logo_text",
		"machine_scope": "边缘与文字", "human_scope": "包装文字目视",
		"protected":       []string{"product_count", "logo_text", "shape", "key_texture", "declared_color"},
		"allowed_regions": []string{"background"},
		"checks":          checks,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func allPassCheckMaps() []map[string]string {
	names := []string{"product_count", "logo_text", "shape", "key_texture", "declared_color"}
	out := make([]map[string]string, len(names))
	for i, n := range names {
		out[i] = map[string]string{"name": n, "result": "pass"}
	}
	return out
}

func TestFidelityRouteHiddenWhenOff(t *testing.T) {
	f := newFixture(t, nil)
	_, login := f.login(t, "fid-off@x.com", "right-pass")
	tok, _ := login["access_token"].(string)
	st, _ := f.do(t, "GET", "/api/v1/projects/proj_x/fidelity-reports", tok, nil)
	if st != http.StatusNotFound {
		t.Fatalf("开关关闭应 404, got %d", st)
	}
}

func TestFidelityHTTPCannotMarkPass(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.SubjectFidelityEnabled = true })
	_, login := f.login(t, "fid-on@x.com", "right-pass")
	tok, _ := login["access_token"].(string)
	st, created := f.do(t, "POST", "/api/v1/projects", tok, map[string]any{"name": "保真工程"})
	if st != http.StatusCreated {
		t.Fatalf("建工程 %d %v", st, created)
	}
	proj := created["project"].(map[string]any)
	id := proj["id"].(string)

	st, denied := f.do(t, "POST", "/api/v1/projects/"+id+"/fidelity-reports", tok,
		fidelityBody(allPassCheckMaps(), map[string]any{"verdict": "pass", "supplier_authorized": true}))
	if st != http.StatusBadRequest {
		t.Fatalf("自报通过应拒绝, got %d %v", st, denied)
	}

	st, saved := f.do(t, "POST", "/api/v1/projects/"+id+"/fidelity-reports", tok, fidelityBody(allPassCheckMaps(), nil))
	if st != http.StatusCreated {
		t.Fatalf("记录应创建, got %d %v", st, saved)
	}
	report := saved["report"].(map[string]any)
	if report["verdict"] != fidelity.VerdictUnknown || report["exact_product"] != false || report["deliverable"] != false {
		t.Fatalf("全通过检查在未授权时仍须待确认: %+v", report)
	}
	if report["real_generation"] != fidelity.RealGenerationUnknown {
		t.Fatalf("真实生成应为 unknown: %+v", report)
	}
	limits, _ := report["limits"].([]any)
	found := false
	for _, item := range limits {
		if item == "真实生成保真未授权，结论为待确认" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少待确认文案: %+v", limits)
	}

	st, dup := f.do(t, "POST", "/api/v1/projects/"+id+"/fidelity-reports", tok, fidelityBody(allPassCheckMaps(), nil))
	if st != http.StatusOK || dup["duplicate"] != true {
		t.Fatalf("同评估应幂等: %d %+v", st, dup)
	}

	checks := allPassCheckMaps()
	checks[1]["result"] = "fail"
	st, failed := f.do(t, "POST", "/api/v1/projects/"+id+"/fidelity-reports", tok,
		fidelityBody(checks, map[string]any{"similarity_only": true}))
	if st != http.StatusCreated {
		t.Fatalf("失败候选应留下: %d %+v", st, failed)
	}
	failReport := failed["report"].(map[string]any)
	if failReport["verdict"] != fidelity.VerdictFail || failReport["deliverable"] != false {
		t.Fatalf("文字失败应保持未通过候选: %+v", failReport)
	}

	st, listed := f.do(t, "GET", "/api/v1/projects/"+id+"/fidelity-reports", tok, nil)
	reports, _ := listed["reports"].([]any)
	if st != http.StatusOK || len(reports) != 2 {
		t.Fatalf("失败记录不得覆盖先前报告: %d %+v", st, listed)
	}

	st, missing := f.do(t, "POST", "/api/v1/projects/"+id+"/fidelity-reports", tok,
		fidelityBody(allPassCheckMaps(), map[string]any{"output_ref": "out_missing"}))
	if st != http.StatusNotFound {
		t.Fatalf("不存在的输出版本应拒绝: %d %+v", st, missing)
	}
}
