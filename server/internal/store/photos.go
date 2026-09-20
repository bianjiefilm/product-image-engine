package store

// HUI-1697 FEAT-0198:产品照片资产仓储。
// 内容幂等键 = (tenant_id, sha256):同租户同内容只入库一次,重复提交幂等返回
// 同一资产引用(不重复登记、不重复计费)。跨租户不可见与不存在同型(防探测红线)。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PhotoAsset 产品照片资产:平台 asset_id 引用 + 服务端核验事实快照。
type PhotoAsset struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	SHA256          string    `json:"sha256"`
	SizeBytes       int64     `json:"size_bytes"`
	MediaType       string    `json:"media_type"`
	WidthPx         int       `json:"width_px"`
	HeightPx        int       `json:"height_px"`
	Purpose         string    `json:"purpose"`
	PlatformAssetID string    `json:"platform_asset_id"`
	OriginalName    string    `json:"original_name"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

var supportedPhotoMedia = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const photoCols = `id, tenant_id, sha256, size_bytes, media_type, width_px, height_px,
	purpose, platform_asset_id, original_name, created_by, created_at`

func scanPhotoAsset(row interface{ Scan(...any) error }) (PhotoAsset, error) {
	var p PhotoAsset
	var created string
	if err := row.Scan(&p.ID, &p.TenantID, &p.SHA256, &p.SizeBytes, &p.MediaType,
		&p.WidthPx, &p.HeightPx, &p.Purpose, &p.PlatformAssetID, &p.OriginalName,
		&p.CreatedBy, &created); err != nil {
		return PhotoAsset{}, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return p, nil
}

func (p *PhotoAsset) validate() error {
	if !sha256Pattern.MatchString(strings.ToLower(p.SHA256)) {
		return fmt.Errorf("%w: sha256 必须是 64 位小写十六进制", ErrValidation)
	}
	if p.SizeBytes <= 0 {
		return fmt.Errorf("%w: size_bytes 必须为正", ErrValidation)
	}
	if !supportedPhotoMedia[p.MediaType] {
		return fmt.Errorf("%w: media_type 不受支持: %q", ErrValidation, p.MediaType)
	}
	if p.WidthPx <= 0 || p.HeightPx <= 0 {
		return fmt.Errorf("%w: 图片尺寸事实缺失", ErrValidation)
	}
	if strings.TrimSpace(p.Purpose) == "" {
		return fmt.Errorf("%w: 用途声明不能为空", ErrValidation)
	}
	if strings.TrimSpace(p.PlatformAssetID) == "" {
		return fmt.Errorf("%w: 平台 asset_id 引用不能为空", ErrValidation)
	}
	return nil
}

// UpsertPhotoAsset 内容幂等入库:同 (tenant, sha256) 已存在 → 原样返回既有行
// (existing=true,新行被忽略,含平台引用);否则插入并返回新行。
func (s *Store) UpsertPhotoAsset(ctx context.Context, in PhotoAsset) (PhotoAsset, bool, error) {
	in.SHA256 = strings.ToLower(strings.TrimSpace(in.SHA256))
	if err := in.validate(); err != nil {
		return PhotoAsset{}, false, err
	}
	if existing, err := s.photoBySHA(ctx, in.TenantID, in.SHA256); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, ErrNotFound) {
		return PhotoAsset{}, false, err
	}
	in.ID = newID("photo")
	now := Now()
	in.CreatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO photo_assets
		(id, tenant_id, sha256, size_bytes, media_type, width_px, height_px,
		 purpose, platform_asset_id, original_name, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.TenantID, in.SHA256, in.SizeBytes, in.MediaType, in.WidthPx, in.HeightPx,
		in.Purpose, in.PlatformAssetID, in.OriginalName, in.CreatedBy, now.Format(time.RFC3339))
	if err != nil {
		// 并发同内容提交:唯一键兜底,回读既有行(幂等语义不依赖先查后插的窗口)。
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			existing, gerr := s.photoBySHA(ctx, in.TenantID, in.SHA256)
			if gerr == nil {
				return existing, true, nil
			}
		}
		return PhotoAsset{}, false, fmt.Errorf("store: 照片资产入库失败: %w", err)
	}
	return in, false, nil
}

func (s *Store) photoBySHA(ctx context.Context, tenantID, sha string) (PhotoAsset, error) {
	p, err := scanPhotoAsset(s.db.QueryRowContext(ctx,
		`SELECT `+photoCols+` FROM photo_assets WHERE tenant_id = ? AND sha256 = ?`, tenantID, sha))
	if errors.Is(err, sql.ErrNoRows) {
		return PhotoAsset{}, ErrNotFound
	}
	if err != nil {
		return PhotoAsset{}, fmt.Errorf("store: 按内容指纹查询照片失败: %w", err)
	}
	return p, nil
}

// GetPhotoAssetBySHA 按(租户, 内容指纹)读取(上传幂等预查入口:同内容重复提交
// 在触达平台登记之前即幂等返回,不重复登记不重复计费)。
func (s *Store) GetPhotoAssetBySHA(ctx context.Context, tenantID, sha string) (PhotoAsset, error) {
	return s.photoBySHA(ctx, tenantID, strings.ToLower(strings.TrimSpace(sha)))
}

// GetPhotoAsset 按(租户, 本地 id)读取;跨租户与不存在同型 ErrNotFound。
func (s *Store) GetPhotoAsset(ctx context.Context, tenantID, id string) (PhotoAsset, error) {
	p, err := scanPhotoAsset(s.db.QueryRowContext(ctx,
		`SELECT `+photoCols+` FROM photo_assets WHERE id = ? AND tenant_id = ?`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return PhotoAsset{}, ErrNotFound
	}
	if err != nil {
		return PhotoAsset{}, fmt.Errorf("store: 查询照片失败: %w", err)
	}
	return p, nil
}

// GetPhotoAssetByAssetID 按(租户, 平台 asset_id 引用)读取(重登恢复解析入口)。
func (s *Store) GetPhotoAssetByAssetID(ctx context.Context, tenantID, platformAssetID string) (PhotoAsset, error) {
	p, err := scanPhotoAsset(s.db.QueryRowContext(ctx,
		`SELECT `+photoCols+` FROM photo_assets WHERE tenant_id = ? AND platform_asset_id = ?`,
		tenantID, platformAssetID))
	if errors.Is(err, sql.ErrNoRows) {
		return PhotoAsset{}, ErrNotFound
	}
	if err != nil {
		return PhotoAsset{}, fmt.Errorf("store: 按平台引用查询照片失败: %w", err)
	}
	return p, nil
}
