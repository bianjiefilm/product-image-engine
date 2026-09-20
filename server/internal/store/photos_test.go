package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newPhotoStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "photo.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func validPhoto() PhotoAsset {
	return PhotoAsset{
		TenantID: "tenant-a", SHA256: strings.Repeat("ab", 32), SizeBytes: 1234,
		MediaType: "image/png", WidthPx: 1, HeightPx: 1, Purpose: "project_photo",
		PlatformAssetID: "ast_1", OriginalName: "photo.png", CreatedBy: "usr_1",
	}
}

func TestUpsertPhotoAssetIdempotentByContent(t *testing.T) {
	st := newPhotoStore(t)
	ctx := context.Background()
	first, existing, err := st.UpsertPhotoAsset(ctx, validPhoto())
	if err != nil || existing {
		t.Fatalf("首次入库失败: existing=%v err=%v", existing, err)
	}
	// 同租户同内容重复提交 → 幂等返回同一资产引用,不重复入库。
	again, existing, err := st.UpsertPhotoAsset(ctx, PhotoAsset{
		TenantID: "tenant-a", SHA256: strings.Repeat("ab", 32), SizeBytes: 1234,
		MediaType: "image/png", WidthPx: 1, HeightPx: 1, Purpose: "project_photo",
		PlatformAssetID: "ast_OTHER", OriginalName: "renamed.png", CreatedBy: "usr_2",
	})
	if err != nil {
		t.Fatalf("重复入库失败: %v", err)
	}
	if !existing {
		t.Fatalf("同内容重复提交必须幂等返回既有行")
	}
	if again.ID != first.ID || again.PlatformAssetID != first.PlatformAssetID {
		t.Fatalf("幂等返回必须同一资产引用: %v vs %v", again, first)
	}
}

func TestPhotoAssetContentDedupIsPerTenant(t *testing.T) {
	st := newPhotoStore(t)
	ctx := context.Background()
	a, _, err := st.UpsertPhotoAsset(ctx, validPhoto())
	if err != nil {
		t.Fatalf("租户 A 入库失败: %v", err)
	}
	// 同内容不同租户:各自持引用(跨租户互不可见,不共享行)。
	b, existing, err := st.UpsertPhotoAsset(ctx, func() PhotoAsset {
		p := validPhoto()
		p.TenantID = "tenant-b"
		return p
	}())
	if err != nil || existing {
		t.Fatalf("租户 B 独立入库失败: existing=%v err=%v", existing, err)
	}
	if b.ID == a.ID {
		t.Fatalf("不同租户同内容不得共享资产行")
	}
}

func TestGetPhotoAssetTenantScoped(t *testing.T) {
	st := newPhotoStore(t)
	ctx := context.Background()
	a, _, err := st.UpsertPhotoAsset(ctx, validPhoto())
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if got, err := st.GetPhotoAsset(ctx, "tenant-a", a.ID); err != nil || got.ID != a.ID {
		t.Fatalf("本租户读取失败: %v %v", got, err)
	}
	// 跨租户不可见:与不存在同型(防租户探测,红线)。
	if _, err := st.GetPhotoAsset(ctx, "tenant-b", a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户读取必须 ErrNotFound: %v", err)
	}
	if _, err := st.GetPhotoAsset(ctx, "tenant-a", "photo_nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的资产必须 ErrNotFound: %v", err)
	}
	byAsset, err := st.GetPhotoAssetByAssetID(ctx, "tenant-a", "ast_1")
	if err != nil || byAsset.ID != a.ID {
		t.Fatalf("按平台 asset_id 读取失败: %v %v", byAsset, err)
	}
	if _, err := st.GetPhotoAssetByAssetID(ctx, "tenant-b", "ast_1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户按平台 asset_id 读取必须不可见: %v", err)
	}
}

func TestUpsertPhotoAssetValidation(t *testing.T) {
	st := newPhotoStore(t)
	ctx := context.Background()
	cases := []struct {
		name string
		mut  func(*PhotoAsset)
	}{
		{"sha256 非十六进制", func(p *PhotoAsset) { p.SHA256 = strings.Repeat("zz", 32) }},
		{"sha256 长度不足", func(p *PhotoAsset) { p.SHA256 = "abcd" }},
		{"媒体类型不受支持", func(p *PhotoAsset) { p.MediaType = "image/gif" }},
		{"用途为空", func(p *PhotoAsset) { p.Purpose = "" }},
		{"尺寸为零", func(p *PhotoAsset) { p.WidthPx = 0 }},
		{"字节数为零", func(p *PhotoAsset) { p.SizeBytes = 0 }},
		{"平台引用为空", func(p *PhotoAsset) { p.PlatformAssetID = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validPhoto()
			c.mut(&p)
			if _, _, err := st.UpsertPhotoAsset(ctx, p); !errors.Is(err, ErrValidation) {
				t.Fatalf("期望 ErrValidation,得到 %v", err)
			}
		})
	}
}
