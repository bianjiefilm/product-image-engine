package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

type revisionBody struct {
	Text          string            `json:"text"`
	Click         string            `json:"click"`
	TargetVersion string            `json:"target_version"`
	Downstream    string            `json:"downstream"`
	Acknowledged  bool              `json:"acknowledged"`
	OriginalB64   string            `json:"original_b64"`
	MaskB64       string            `json:"mask_b64"`
	BriefVersion  string            `json:"brief_version"`
	Facts         map[string]string `json:"facts"`
	Status        string            `json:"status"`
	Version       string            `json:"version"`
	Target        string            `json:"target"`
}

func (s *Server) revisionFor(w http.ResponseWriter, r *http.Request) (store.Project, *revision.Ledger, bool) {
	p, _ := principalFrom(r.Context())
	proj, err := s.projectFor(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return store.Project{}, nil, false
	}
	snap, err := s.St.LoadRevisionLedger(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeRevisionErr(w, err)
		return store.Project{}, nil, false
	}
	led, err := revision.Restore(snap)
	if err != nil {
		writeRevisionErr(w, err)
		return store.Project{}, nil, false
	}
	return proj, led, true
}

func (s *Server) saveRevision(w http.ResponseWriter, r *http.Request, proj store.Project, led *revision.Ledger) bool {
	if err := s.St.SaveRevisionLedger(r.Context(), proj.TenantID, proj.ID, led.Snapshot()); err != nil {
		writeRevisionErr(w, err)
		return false
	}
	return true
}

func (s *Server) handleRevisionIntent(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.revisionFor(w, r); !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	intent, err := intentFromBody(body)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	cost, err := revision.IncrementalCostCents(intent)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intent": intent, "incremental_cost_cents": cost})
}

func (s *Server) handleRevisionPlan(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.revisionFor(w, r); !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	intent, err := intentFromBody(body)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	plan, err := revision.PlanRevision(intent, revision.Provider{PartialEdit: false})
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	_, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, revisionState(led))
}

func (s *Server) handleCreateRevision(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	intent, err := intentFromBody(body)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	switch intent.Action {
	case revision.ActionSimplifyBackground, revision.ActionOutdoor, revision.ActionBrightenKeepProduct:
		plan, err := revision.PlanRevision(intent, revision.Provider{PartialEdit: false})
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		if plan.RequiresAcknowledgement && !body.Acknowledged {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": map[string]string{
					"code": "impact_unacknowledged", "message": plan.ImpactNotice,
				},
				"impact_notice":          plan.ImpactNotice,
				"incremental_cost_cents": plan.IncrementalCostCents,
			})
			return
		}
		original, err := decodeImageField(body.OriginalB64)
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		mask, err := decodeImageField(body.MaskB64)
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		result, err := revision.Execute(revision.ExecuteRequest{
			Intent: intent, Provider: revision.Provider{PartialEdit: false},
			Acknowledged: body.Acknowledged, Original: original, Mask: mask,
		})
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		version, err := led.PutCandidate(result.PNG, intent, result.Origin, body.BriefVersion)
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		if !s.saveRevision(w, r, proj, led) {
			return
		}
		version.PNG = nil
		writeJSON(w, http.StatusCreated, map[string]any{
			"version": version, "subject_preserved": result.SubjectPreserved,
			"supplier_calls": result.SupplierCalls, "origin": result.Origin,
			"incremental_cost_cents": result.Plan.IncrementalCostCents,
			"steps_run":              result.StepsRun, "skipped": result.Plan.Skipped,
		})
	case revision.ActionRollback:
		if err := led.Rollback(intent.TargetVersion); err != nil {
			writeRevisionErr(w, err)
			return
		}
		if !s.saveRevision(w, r, proj, led) {
			return
		}
		writeJSON(w, http.StatusOK, revisionState(led))
	case revision.ActionAdopt:
		if err := led.Adopt(body.Version); err != nil {
			writeRevisionErr(w, err)
			return
		}
		if !s.saveRevision(w, r, proj, led) {
			return
		}
		writeJSON(w, http.StatusOK, revisionState(led))
	case revision.ActionSendDownstream:
		ref, err := led.Downstream(body.Version, intent.Downstream)
		if err != nil {
			writeRevisionErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"downstream": ref})
	default:
		writeRevisionErr(w, revision.ErrUnrecognized)
	}
}

func (s *Server) handleRevisionOutcome(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	intent, err := intentFromBody(body)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	if _, err := led.RecordUnresolved(body.Status, intent); err != nil {
		writeRevisionErr(w, err)
		return
	}
	if !s.saveRevision(w, r, proj, led) {
		return
	}
	writeJSON(w, http.StatusOK, revisionState(led))
}

func (s *Server) handleRevisionBrief(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	diff, err := led.ProposeBrief(body.Version, body.Facts)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	if !s.saveRevision(w, r, proj, led) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": diff})
}

func (s *Server) handleRevisionRollback(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	intent, err := intentFromBody(body)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	target := intent.TargetVersion
	if target == "" {
		target = body.Version
	}
	if intent.Action != revision.ActionRollback && body.Version == "" {
		writeRevisionErr(w, revision.ErrUnrecognized)
		return
	}
	if err := led.Rollback(target); err != nil {
		writeRevisionErr(w, err)
		return
	}
	if !s.saveRevision(w, r, proj, led) {
		return
	}
	writeJSON(w, http.StatusOK, revisionState(led))
}

func (s *Server) handleRevisionCompare(w http.ResponseWriter, r *http.Request) {
	_, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	pair, err := led.SideBySide(r.URL.Query().Get("left"), r.URL.Query().Get("right"))
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comparison": pair})
}

func (s *Server) handleRevisionAdopt(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	if err := led.Adopt(r.PathValue("label")); err != nil {
		writeRevisionErr(w, err)
		return
	}
	if !s.saveRevision(w, r, proj, led) {
		return
	}
	writeJSON(w, http.StatusOK, revisionState(led))
}

func (s *Server) handleRevisionExport(w http.ResponseWriter, r *http.Request) {
	_, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	ref, err := led.Export(r.PathValue("label"))
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"export": ref})
}

func (s *Server) handleRevisionDownstream(w http.ResponseWriter, r *http.Request) {
	_, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	body, ok := readRevisionBody(w, r)
	if !ok {
		return
	}
	ref, err := led.Downstream(r.PathValue("label"), body.Target)
	if err != nil {
		writeRevisionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"downstream": ref})
}

func (s *Server) handleRevisionContent(w http.ResponseWriter, r *http.Request) {
	_, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	label := r.PathValue("label")
	for _, v := range led.Versions {
		if v.Label != label {
			continue
		}
		if len(v.PNG) == 0 {
			writeRevisionErr(w, revision.ErrNotExportable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Revision-Version", v.Label)
		w.Header().Set("X-Revision-Sha256", v.ContentHash)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(v.PNG)
		return
	}
	writeRevisionErr(w, revision.ErrExplicitVersion)
}

func revisionState(led *revision.Ledger) map[string]any {
	views := make([]revision.Version, 0, len(led.Versions))
	for _, v := range led.Versions {
		v.PNG = nil
		views = append(views, v)
	}
	return map[string]any{
		"adopted_label":        led.AdoptedID,
		"adopted_content_hash": led.AdoptedHash(),
		"versions":             views,
	}
}

func readRevisionBody(w http.ResponseWriter, r *http.Request) (revisionBody, bool) {
	var body revisionBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
	if err := dec.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return revisionBody{}, false
	}
	return body, true
}

func intentFromBody(body revisionBody) (revision.Intent, error) {
	switch {
	case body.Text != "" && body.Click != "":
		return revision.Intent{}, revision.ErrUnrecognized
	case body.Text != "":
		return revision.ParseNaturalLanguage(body.Text)
	case body.Click != "":
		return revision.FromClick(body.Click, body.TargetVersion, body.Downstream)
	default:
		return revision.Intent{}, revision.ErrUnrecognized
	}
}

func decodeImageField(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, revision.ErrBadImage
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 || len(raw) > 4<<20 {
		return nil, revision.ErrBadImage
	}
	return raw, nil
}

func writeRevisionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, revision.ErrImpactUnacknowledged):
		writeErr(w, http.StatusConflict, "impact_unacknowledged", revision.ImpactNotice)
	case errors.Is(err, revision.ErrPreserveSubject):
		writeErr(w, http.StatusConflict, "preserve_subject", err.Error())
	case errors.Is(err, revision.ErrNotExportable):
		writeErr(w, http.StatusConflict, "not_exportable", "这个版本不能采用或导出")
	case errors.Is(err, revision.ErrUnrecognized):
		writeErr(w, http.StatusBadRequest, "unrecognized_revision", "认不出的修改，不会整图重画")
	case errors.Is(err, revision.ErrExplicitVersion):
		writeErr(w, http.StatusBadRequest, "explicit_version_required", "必须点名一个版本")
	case errors.Is(err, revision.ErrUnknownDownstream):
		writeErr(w, http.StatusBadRequest, "unknown_downstream", "下游只接受数字人、AiCut 或矩阵")
	case errors.Is(err, revision.ErrBadImage):
		writeErr(w, http.StatusBadRequest, "invalid_image", "原图或蒙版不是可用的 PNG")
	case errors.Is(err, revision.ErrBadBrief):
		writeErr(w, http.StatusBadRequest, "invalid_brief", "Brief 差异无法读取")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrValidation), errors.Is(err, store.ErrConflict):
		writeStoreErr(w, err)
	default:
		writeErr(w, http.StatusInternalServerError, "internal", "局部修改没有完成")
	}
}
