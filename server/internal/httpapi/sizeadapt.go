package httpapi

// 电商尺寸适配端点(HUI-1703 / FEAT-0204)。
//
// 红线(票面):
//   - 纯确定性图像处理:不调用任何 AI 模型、不产生 Task/Billing 调用边、
//     不读写生成开关(FEATURE_GENERATION_ENABLED off 不影响本域);
//   - FEATURE_SIZE_ADAPT off → 路由不注册(server.go)= 404 不可见;
//   - 变体唯一键 = (source_output_id, preset_name, mode) 三元组:重复请求
//     幂等返回既有变体,不重复变换、不重复登记;
//   - 租户门控沿用工程域:跨租户/非属主 403(无串数据),不存在 404;
//   - 输入只接受本工程已登记的合法 PNG 成果(sha256 内容指纹绑定)。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sizeadapt"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// ---- GET /api/v1/size-adapt/presets -----------------------------------------

func (s *Server) handleListSizePresets(w http.ResponseWriter, r *http.Request) {
	if s.Presets == nil {
		writeErr(w, http.StatusServiceUnavailable, "presets_unavailable", "尺寸适配预设集未装配,拒绝服务(fail-closed)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": s.Presets.List()})
}

// ---- POST /api/v1/projects/{id}/size-adapt ----------------------------------

type sizeAdaptRequest struct {
	SourceOutputID string `json:"source_output_id"`
	PresetName     string `json:"preset_name"`
	Format         string `json:"format"` // 可选:png(默认)|jpeg(quality 固定 90)
	DataB64        string `json:"data_b64"`
}

func (s *Server) handleCreateSizeVariant(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	var req sizeAdaptRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON(source_output_id/preset_name/data_b64)")
		return
	}
	if strings.TrimSpace(req.SourceOutputID) == "" || strings.TrimSpace(req.PresetName) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "source_output_id 与 preset_name 必填")
		return
	}
	format := sizeadapt.FormatPNG
	if f := strings.ToLower(strings.TrimSpace(req.Format)); f != "" {
		format = sizeadapt.Format(f)
	}
	if format != sizeadapt.FormatPNG && format != sizeadapt.FormatJPEG {
		writeErr(w, http.StatusBadRequest, "invalid_request", "format 仅支持 png/jpeg")
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.DataB64)
	if err != nil || len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "invalid_request", "data_b64 必须是非空 base64")
		return
	}
	if len(data) > sizeadapt.MaxInputBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "源图片超过 8 MiB 上限")
		return
	}
	tenant := p.Tenant()
	ctx := r.Context()
	projID := r.PathValue("id")

	// 1) 工程租户门控(403/404 分类;Unscoped 仅分类,绝不回写他租户数据)。
	if done := s.writeProjectAccessErr(w, ctx, tenant, projID); done {
		return
	}
	// 2) 源输出必须属于本工程且是登记成果(kind=result,变体不再级联派生)。
	src, err := s.St.GetOutput(ctx, tenant, req.SourceOutputID)
	if err != nil || src.ProjectID != projID {
		writeErr(w, http.StatusNotFound, "not_found", "源成果不存在(或不属于该工程)")
		return
	}
	if src.Kind != store.OutputKindResult {
		writeErr(w, http.StatusBadRequest, "invalid_request", "源必须是登记成果(kind=result),尺寸变体不可再派生")
		return
	}
	// 3) 内容指纹绑定:data_b64 必须就是登记时的那份字节。
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != src.ResultSHA256 {
		writeErr(w, http.StatusBadRequest, "sha_mismatch",
			"携带内容与已登记成果指纹不符(sha256);请回传登记时的原始 PNG 字节")
		return
	}
	// 4) 预设必须在集合内(预设集由 main 装配并整体校验)。
	if s.Presets == nil {
		writeErr(w, http.StatusServiceUnavailable, "presets_unavailable", "尺寸适配预设集未装配,拒绝服务(fail-closed)")
		return
	}
	preset, ok := s.Presets.Get(strings.TrimSpace(req.PresetName))
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown_preset", "未知预设:"+req.PresetName+"(GET /api/v1/size-adapt/presets 查看可用列表)")
		return
	}
	// 5) 幂等裁决:同 (源,预设,模式) 已有变体 → 原样返回,不重复变换/登记。
	//    (首个变体的输出格式胜出:格式不在唯一键内,拍板口径见计划 §1.3。)
	if existing, err := s.St.FindSizeVariantByKey(ctx, tenant, src.ID, preset.Name, string(preset.Mode)); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"variant": existing, "duplicate": true})
		return
	}
	// 6) 输入门(魔数/大小/解码/像素上限;分类错误 → 400/413)。
	img, err := sizeadapt.DecodePNG(data)
	if err != nil {
		writeSizeInputErr(w, err)
		return
	}
	// 7) 平台登记 fail-closed(与生成/计费开关无关;登记失败不落变体)。
	if s.Uploads == nil || s.Cfg.UploadBaseURL == "" || s.Cfg.UploadToken == "" {
		writeErr(w, http.StatusServiceUnavailable, "upload_not_configured",
			"素材登记服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN),不伪造登记成功")
		return
	}
	outBytes, vw, vh, err := sizeadapt.Adapt(img, preset, format)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "尺寸变换失败:"+err.Error())
		return
	}
	reg, err := s.Uploads.RegisterAsset(ctx, platform.RegisterAssetRequest{
		FileName: variantFileName(src.FileName, preset.Name, format),
		ContentType: mediaTypeFor(format), SizeBytes: int64(len(outBytes)),
		SHA256: sha256Hex(outBytes), ContentB64: base64.StdEncoding.EncodeToString(outBytes),
	})
	if err != nil {
		s.mapPlatformErr(w, "素材登记", err)
		return
	}
	// 8) 落变体行 + 内容字节(单事务;唯一键兜底并发,冲突即重读幂等返回)。
	variant, err := s.St.CreateSizeVariant(ctx, store.ProjectOutput{
		TenantScope: tenant, ProjectID: projID,
		PlatformAssetID: reg.AssetID,
		ResultSHA256:    sha256Hex(outBytes), ResultSize: int64(len(outBytes)),
		MediaType: mediaTypeFor(format),
		FileName:  variantFileName(src.FileName, preset.Name, format),
		SourceOutputID: src.ID, PresetName: preset.Name,
		VariantMode: string(preset.Mode), VariantWidth: vw, VariantHeight: vh,
	}, outBytes)
	if errors.Is(err, store.ErrConflict) {
		if existing, gerr := s.St.FindSizeVariantByKey(ctx, tenant, src.ID, preset.Name, string(preset.Mode)); gerr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"variant": existing, "duplicate": true})
			return
		}
		writeErr(w, http.StatusConflict, "variant_conflict", "同键变体并发写入冲突,请重试")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"variant": variant, "duplicate": false})
}

// ---- GET /api/v1/projects/{id}/size-adapt -----------------------------------

func (s *Server) handleListSizeVariants(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	projID := r.PathValue("id")
	if done := s.writeProjectAccessErr(w, ctx, p.Tenant(), projID); done {
		return
	}
	list, err := s.St.ListSizeVariants(ctx, p.Tenant(), projID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取尺寸变体列表失败")
		return
	}
	if list == nil {
		list = []store.ProjectOutput{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"variants": list})
}

// ---- GET /api/v1/projects/{id}/size-adapt/{variantId}/download --------------

func (s *Server) handleDownloadSizeVariant(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	projID, variantID := r.PathValue("id"), r.PathValue("variantId")
	if done := s.writeProjectAccessErr(w, ctx, p.Tenant(), projID); done {
		return
	}
	variant, err := s.St.GetOutput(ctx, p.Tenant(), variantID)
	if err != nil || variant.ProjectID != projID || variant.Kind != store.OutputKindSizeVariant {
		writeErr(w, http.StatusNotFound, "not_found", "尺寸变体不存在(或不属于该工程)")
		return
	}
	content, err := s.St.GetVariantBlob(ctx, p.Tenant(), variantID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	mediaType := variant.MediaType
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", asciiFileName(variant.FileName, variantID, mediaType)))
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	_, _ = w.Write(content)
}

// ---- 共用小件 ----------------------------------------------------------------

// writeProjectAccessErr 工程访问门:本租户不存在 → 404;全局存在但属他租户
// → 403 tenant_mismatch(沿用 handoff 域口径)。返回 true 表示已写响应。
// Unscoped 读取仅用于分类,响应里绝不出现他租户行数据(红线)。
func (s *Server) writeProjectAccessErr(w http.ResponseWriter, ctx context.Context, tenant, projID string) bool {
	if _, err := s.St.GetProject(ctx, tenant, projID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if _, uerr := s.St.GetProjectUnscoped(ctx, projID); uerr == nil {
				writeErr(w, http.StatusForbidden, "tenant_mismatch", "工程不属于当前账号(拒绝,无串数据)")
				return true
			}
		}
		writeStoreErr(w, err)
		return true
	}
	return false
}

// writeSizeInputErr 尺寸输入门错误 → HTTP 状态(413 超限,其余 400)。
func writeSizeInputErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sizeadapt.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, sizeadapt.ErrBadMagic), errors.Is(err, sizeadapt.ErrCorrupt),
		errors.Is(err, sizeadapt.ErrHugePixels):
		writeErr(w, http.StatusBadRequest, "invalid_image", err.Error())
	default:
		writeErr(w, http.StatusBadRequest, "invalid_image", "源图片被拒绝:"+err.Error())
	}
}

func mediaTypeFor(f sizeadapt.Format) string {
	if f == sizeadapt.FormatJPEG {
		return "image/jpeg"
	}
	return "image/png"
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// variantFileName 确定性变体文件名:<源名去扩展名>-<预设>.<ext>。
// 源名为空/不可用时退化为 variant-<预设>.<ext>。
func variantFileName(srcFileName, presetName string, f sizeadapt.Format) string {
	base := path.Base(srcFileName)
	base = strings.TrimSuffix(base, path.Ext(base))
	return fmt.Sprintf("%s-%s.%s", asciiFileName(base, "variant", ""), presetName, extFor(f))
}

func extFor(f sizeadapt.Format) string {
	if f == sizeadapt.FormatJPEG {
		return "jpg"
	}
	return "png"
}

// asciiFileName 文件名消毒:仅保留 [A-Za-z0-9._-],其余折叠为 '-'(HTTP 头安全)。
// fallbackID 用于整名为空时的兜底(按 ID 仍可定位)。
func asciiFileName(name, fallbackID, mediaType string) string {
	var b strings.Builder
	lastDash := false
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		ext := ""
		switch mediaType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		}
		out = fallbackID + ext
	}
	return out
}
