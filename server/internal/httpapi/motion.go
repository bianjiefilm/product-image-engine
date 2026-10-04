package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/motionref"
	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

type motionDeclareBody struct {
	RevisionID string `json:"revision_id"`
}

type motionParamBody struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type motionBatchBody struct {
	Variants []struct {
		ID             string `json:"id"`
		ExecutionCount *int   `json:"execution_count"`
	} `json:"variants"`
}

func (s *Server) handleCreateMotionRef(w http.ResponseWriter, r *http.Request) {
	proj, led, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	var body motionDeclareBody
	if !readMotionJSON(w, r, &body) {
		return
	}
	asset, err := adoptedMotionAsset(led, body.RevisionID)
	if err != nil {
		writeMotionErr(w, err)
		return
	}
	declared, err := motionref.Declare(asset)
	if err != nil {
		writeMotionErr(w, err)
		return
	}
	saved, created, err := s.St.PutMotionDeclaration(r.Context(), proj.TenantID, proj.ID, declared)
	if err != nil {
		writeMotionErr(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"declaration": saved, "created": created})
}

func (s *Server) handleListMotionRefs(w http.ResponseWriter, r *http.Request) {
	proj, _, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	list, err := s.St.ListMotionDeclarations(r.Context(), proj.TenantID, proj.ID)
	if err != nil {
		writeMotionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"declarations": list})
}

func (s *Server) handleMotionParameter(w http.ResponseWriter, r *http.Request) {
	proj, _, ok := s.revisionFor(w, r)
	if !ok {
		return
	}
	var body motionParamBody
	if !readMotionJSON(w, r, &body) {
		return
	}
	saved, err := s.St.RecordMotionParameter(r.Context(), proj.TenantID, proj.ID, r.PathValue("refId"), body.Name, body.Value)
	if err != nil {
		writeMotionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"declaration": saved})
}

func (s *Server) handleMotionBatchRerun(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.revisionFor(w, r); !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体无法读取")
		return
	}
	var body motionBatchBody
	if len(bytes.TrimSpace(raw)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON，且不能带生成、视频或供应商字段")
			return
		}
	}
	others := make([]motionref.VariantExecution, 0, len(body.Variants))
	for _, v := range body.Variants {
		others = append(others, motionref.VariantExecution{ID: v.ID, ExecutionCount: v.ExecutionCount})
	}
	if err := motionref.BatchRerun(others); err != nil {
		writeMotionErr(w, err)
		return
	}
	writeErr(w, http.StatusConflict, "batch_rerun_unproven", "HUI-2732 未完成，不能证明其他变体的执行次数为 0，所以不批量重跑。还没生成成片。")
}

func adoptedMotionAsset(led *revision.Ledger, revisionID string) (motionref.AdoptedAsset, error) {
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" || revisionID != led.AdoptedID {
		return motionref.AdoptedAsset{}, motionref.ErrNotAdopted
	}
	for _, v := range led.Versions {
		if v.Label != revisionID {
			continue
		}
		if v.Outcome != revision.StatusCandidate || v.AssetRef == "" || v.ContentHash == "" {
			return motionref.AdoptedAsset{}, motionref.ErrNotAdopted
		}
		return motionref.AdoptedAsset{
			AssetID: v.AssetRef, ContentHash: v.ContentHash, RevisionID: v.Label, Adopted: true,
		}, nil
	}
	return motionref.AdoptedAsset{}, motionref.ErrNotAdopted
}

func readMotionJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON，且不能带生成、视频或供应商字段")
		return false
	}
	return true
}

func writeMotionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, motionref.ErrNotAdopted):
		writeErr(w, http.StatusConflict, "not_adopted", "只有已采用的商品资产才能形成 Motion 引用。还没生成成片。")
	case errors.Is(err, motionref.ErrBadParameter):
		writeErr(w, http.StatusBadRequest, "invalid_parameter", "改价格或颜色只记录参数，不会重做商品。")
	case errors.Is(err, motionref.ErrBadAsset):
		writeErr(w, http.StatusBadRequest, "invalid_request", "已采用商品资产不完整，不能形成引用。")
	case errors.Is(err, motionref.ErrUnverifiedAd):
		writeErr(w, http.StatusConflict, "not_a_dynamic_ad", "还没生成成片。没有核验过的引用不能当成动态广告。")
	case errors.Is(err, motionref.ErrBatchUnproven):
		writeErr(w, http.StatusConflict, "batch_rerun_unproven", "HUI-2732 未完成，不能证明其他变体的执行次数为 0，所以不批量重跑。还没生成成片。")
	case errors.Is(err, motionref.ErrDrift), errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "subject_unchanged", "主体哈希不能改。还没生成成片。")
	case errors.Is(err, motionref.ErrDigest):
		writeErr(w, http.StatusConflict, "bad_digest", "引用摘要必须是 sha256 加 64 位十六进制。")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrValidation):
		writeStoreErr(w, err)
	default:
		writeErr(w, http.StatusInternalServerError, "internal", "Motion 引用没有形成。还没生成成片。")
	}
}
