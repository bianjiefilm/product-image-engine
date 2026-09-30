package store

import (
	"context"
	"errors"
	"testing"
)

func TestSaveBgModelBytesKeepsBillingClosed(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := insertSubmittingBg(t, s)
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	saved, already, err := s.SaveBgModelBytes(ctx, BgModelBytes{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		MediaType: "image/png", Bytes: png, Pending: []string{"真实出图未完成"},
	})
	if err != nil || already || saved.JobStatus != "completed" || saved.Deliverable || saved.Quality == "pass" {
		t.Fatalf("解码字节不能写成可交付或保真通过: already=%v err=%v %#v", already, err, saved)
	}
	if saved.Origin != "model_http" || saved.Origin == "supplier" || saved.OutputAssetID == "" || saved.PlatformTaskID != "" || saved.FeeRef != "" {
		t.Fatalf("来源不能写成供应商成功或计费通过: %#v", saved)
	}
	media, body, err := s.OpenBgResult(ctx, job.TenantID, job.ProjectID, job.ID)
	if err != nil || media != "image/png" || string(body) != string(png) {
		t.Fatalf("应读回解码字节: %v %s %q", err, media, body)
	}
	again, already, err := s.SaveBgModelBytes(ctx, BgModelBytes{
		TenantID: job.TenantID, ProjectID: job.ProjectID, JobID: job.ID,
		MediaType: "image/png", Bytes: []byte("other"), Pending: []string{"晚到"},
	})
	if err != nil || !already || again.OutputAssetID != saved.OutputAssetID {
		t.Fatalf("已完成记录不能再写一遍: already=%v err=%v %#v", already, err, again)
	}
	media, body, err = s.OpenBgResult(ctx, job.TenantID, job.ProjectID, job.ID)
	if err != nil || string(body) != string(png) {
		t.Fatalf("晚到字节不能覆盖: %v %q", err, body)
	}

	failed := insertSubmittingBg(t, s)
	failed.JobStatus = "failed"
	if err := s.UpdateBgJob(ctx, failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveBgModelBytes(ctx, BgModelBytes{
		TenantID: failed.TenantID, ProjectID: failed.ProjectID, JobID: failed.ID,
		MediaType: "image/png", Bytes: png,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("失败记录不能改成完成, got %v", err)
	}
}

func insertSubmittingBg(t *testing.T, s *Store) BgJob {
	t.Helper()
	job, err := s.InsertBgJob(context.Background(), BgJob{
		TenantID: "ten", ProjectID: "prj", InputID: "pin", InputVersion: "v1",
		Mode: "creative", ProtectedRegion: "logo,packaging_text,spec,structure",
		BackgroundIntent: "浅灰棚拍", Fingerprint: "fp-" + newID("x"),
		QuoteStatus: "confirmed", BillingLabel: "配置缺失",
		JobStatus: "submitting", Quality: "unknown", Evidence: "none",
		Pending: []string{"主体质量待确认"}, AllowedUses: []string{"预览"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}
