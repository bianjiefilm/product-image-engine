package httpapi

// HUI-1697 FEAT-0198:产品照片上传(服务端单点校验链)。
// 纪律:
//   - BFF 只做受限中继,业务校验全部在本层(服务端单点);
//   - 主体/租户只认已验证平台身份(JWT),客户端自报一律不作数;
//   - 用途 purpose 是封闭枚举;大小上限配置化(PRODUCT_PHOTO_MAX_BYTES,默认 20MiB);
//   - 媒体类型以魔数嗅探 + 真解码为准,客户端扩展名/Content-Type 一律不作数;
//   - 内容幂等:同租户同 sha256 重复提交幂等返回同一资产引用,不重复触平台登记
//     (不重复入库、不重复计费);断点续传 v1 不做——整文件上传,失败重传走幂等;
//   - 登记未完成(平台 complete 未过/本地未落库)的资产绝不可能成为生成输入:
//     photo_assets 行只在完整登记成功后存在;
//   - 日志脱敏:只记 hash/size/媒体类型,绝不落文件内容或文件名原文。

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/imgprobe"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// validPhotoPurposes 用途声明封闭枚举(扩展新用途=在此登记并补测试)。
var validPhotoPurposes = map[string]bool{"project_photo": true}

// handlePhotoUpload POST /api/v1/photos?purpose=project_photo&filename=x.png
// 请求体 = 原始图片字节(无 multipart;Content-Type 头不作数)。
func (s *Server) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.PhotoUploadEnabled {
		writeErr(w, http.StatusServiceUnavailable, "photo_upload_disabled",
			"照片上传开关未开启(FEATURE_PHOTO_UPLOAD=0)")
		return
	}
	if s.Cfg.UploadBaseURL == "" || s.Cfg.UploadToken == "" {
		writeErr(w, http.StatusServiceUnavailable, "upload_unconfigured",
			"素材上传服务未配置(缺少 PLATFORM_UPLOAD_BASE_URL 或 PLATFORM_UPLOAD_TOKEN)")
		return
	}
	p, _ := principalFrom(r.Context())
	ctx := r.Context()

	// 1) 用途声明(封闭枚举,防止“任意用途”绕过授权语义)。
	purpose := strings.TrimSpace(r.URL.Query().Get("purpose"))
	if !validPhotoPurposes[purpose] {
		writeErr(w, http.StatusBadRequest, "invalid_purpose",
			"用途声明必须是受支持枚举(当前: project_photo)")
		return
	}
	// 文件名只作平台侧展示元数据,绝不参与格式判定;剥离路径成分。
	filename := sanitizePhotoFilename(r.URL.Query().Get("filename"))

	// 2) 大小上限(服务端单点,配置化)。
	max := s.Cfg.PhotoUploadMaxBytes()
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "photo_too_large",
			fmt.Sprintf("照片超过大小上限(%d 字节),已拒绝", max))
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "empty_body", "请求体为空,必须携带图片字节")
		return
	}

	// 3) 内容核验:魔数嗅探 + 真解码(jpeg/png 标准库,webp 纯 Go 解码器)。
	meta, err := imgprobe.Probe(data)
	if errors.Is(err, imgprobe.ErrUnsupported) {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"内容不是受支持的图片格式(仅 jpeg/png/webp),客户端声明不作数")
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "undecodable_image",
			"图片内容无法解码,已拒绝(不接受不可执行输入)")
		return
	}
	sum := sha256.Sum256(data)
	shaHex := hex.EncodeToString(sum[:])

	// 4) 内容幂等预查:同租户同内容直接返回既有引用,不触平台登记。
	if existing, err := s.St.GetPhotoAssetBySHA(ctx, p.Tenant(), shaHex); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"photo": existing, "idempotent": true})
		return
	}

	// 5) 经平台 upload 设施登记(申报 sha256/size/属主 principal;平台侧 complete
	//    再按对象头核验,双保险)。失败 fail-closed:本地零落库,资产永不成输入。
	principal := platform.PhotoPrincipal{Type: "user", ID: p.UserID}
	assetID, err := s.Uploads.RegisterPhoto(ctx, principal, filename, meta.MediaType, data, shaHex)
	if err != nil {
		if errors.Is(err, platform.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "upstream_unavailable",
				"素材上传服务不可达,已拒绝本次登记(不伪造成功)")
			return
		}
		writeErr(w, http.StatusBadGateway, "upstream_rejected",
			"素材上传服务拒绝了本次登记:"+err.Error())
		return
	}

	// 6) 本地落库(内容幂等键 tenant+sha256;并发同内容由唯一键兜底回读)。
	pa, existing, err := s.St.UpsertPhotoAsset(ctx, store.PhotoAsset{
		TenantID: p.Tenant(), SHA256: shaHex, SizeBytes: int64(len(data)),
		MediaType: meta.MediaType, WidthPx: meta.WidthPx, HeightPx: meta.HeightPx,
		Purpose: purpose, PlatformAssetID: assetID, OriginalName: filename, CreatedBy: p.UserID,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// 日志脱敏:只有 hash/size/媒体类型。
	log.Printf("photo upload: sha256=%s size=%d media=%s purpose=%s idempotent=%v",
		shaHex, len(data), meta.MediaType, purpose, existing)
	writeJSON(w, statusForCreate(existing), map[string]any{"photo": pa, "idempotent": existing})
}

// handlePhotoContent GET /api/v1/photos/{id}/content — 本租户照片解析为真实字节。
func (s *Server) handlePhotoContent(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	pa, err := s.St.GetPhotoAsset(r.Context(), p.Tenant(), r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err) // 跨租户与不存在同型 404(防探测红线)
		return
	}
	s.streamPhotoAsset(w, r, pa.PlatformAssetID, &pa)
}

// handleInputContent GET /api/v1/projects/{id}/inputs/{inputId}/content —
// 工程内版本引用解析:本租户照片(带完整性复检)或跨应用授权引用
// (经平台设施解析,本产品绝不读其他应用 SQLite/共享目录)。
func (s *Server) handleInputContent(w http.ResponseWriter, r *http.Request) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	projID, inputID := r.PathValue("id"), r.PathValue("inputId")
	if _, err := s.St.GetProject(ctx, p.Tenant(), projID); err != nil {
		s.writeProjectAccessErr(w, ctx, p.Tenant(), projID)
		return
	}
	inputs, err := s.St.ListInputs(ctx, p.Tenant(), projID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "读取输入失败")
		return
	}
	var found *store.ProjectInput
	for i := range inputs {
		if inputs[i].ID == inputID {
			found = &inputs[i]
			break
		}
	}
	if found == nil {
		writeErr(w, http.StatusNotFound, "not_found", "输入不存在(或不属于当前账号)")
		return
	}
	// 本租户照片行 → 完整性复检后解析;无本地图(跨应用授权引用)→ 直接经平台解析。
	pa, err := s.St.GetPhotoAssetByAssetID(ctx, p.Tenant(), found.PlatformAssetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.streamPhotoAsset(w, r, found.PlatformAssetID, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal", "查询照片失败")
		return
	}
	s.streamPhotoAsset(w, r, pa.PlatformAssetID, &pa)
}

// streamPhotoAsset 经平台设施解析真实字节:
// 资产元数据 → 完整性复检(HUI-1732 口径:申报事实逐字段比对,不符即拒)→
// 受限下载地址 → 字节流式回传(日志只有 hash/size)。
func (s *Server) streamPhotoAsset(w http.ResponseWriter, r *http.Request, assetID string, pa *store.PhotoAsset) {
	p, _ := principalFrom(r.Context())
	ctx := r.Context()
	principal := platform.PhotoPrincipal{Type: "user", ID: p.UserID}

	meta, err := s.Uploads.PhotoAsset(ctx, principal, assetID)
	if err != nil {
		s.mapPlatformErr(w, "素材上传", err)
		return
	}
	if pa != nil {
		mismatch := !strings.EqualFold(meta.SHA256, pa.SHA256) ||
			meta.SizeBytes != pa.SizeBytes ||
			(meta.ContentType != "" && meta.ContentType != pa.MediaType)
		if mismatch {
			writeErr(w, http.StatusBadGateway, "integrity_failed",
				"平台资产事实与本地登记不符,已拒绝解析(完整性保护)")
			return
		}
	}
	dlURL, dlCT, _, err := s.Uploads.PhotoDownloadURL(ctx, principal, assetID, 0)
	if err != nil {
		s.mapPlatformErr(w, "素材上传", err)
		return
	}
	rc, ct, clen, err := s.Uploads.OpenPhotoContent(ctx, dlURL)
	if err != nil {
		s.mapPlatformErr(w, "素材上传", err)
		return
	}
	defer rc.Close()
	if ct == "" {
		ct = dlCT
	}
	if ct == "" {
		ct = meta.ContentType
	}
	// 只以图片类型回传(显示语义),其余一律 octet-stream;绝不当作可执行输入。
	if !strings.HasPrefix(ct, "image/") {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if clen > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(clen))
	} else if meta.SizeBytes > 0 {
		w.Header().Set("Content-Length", fmt.Sprint(meta.SizeBytes))
	} else {
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// sanitizePhotoFilename 剥离路径成分并限长;只作元数据,不参与任何安全判定。
func sanitizePhotoFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	const maxName = 200
	if len(name) > maxName {
		name = name[len(name)-maxName:] // 保留尾部(扩展名仅为展示元数据)
	}
	if name == "" {
		return "photo"
	}
	return name
}

func statusForCreate(idempotent bool) int {
	if idempotent {
		return http.StatusOK
	}
	return http.StatusCreated
}
