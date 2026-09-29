package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/textimage"
)

func fixtureJob(t *testing.T, s *Store, status string) TextImageJob {
	t.Helper()
	job, err := s.InsertTextImageJob(context.Background(), TextImageJob{
		TenantID: "ten", ProjectID: "prj", Prompt: "fixture 杯子",
		Fingerprint: "fp-fixture", QuoteStatus: textimage.QuoteConfirmed, BillingLabel: "配置缺失",
		JobStatus: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func fixtureBytes() ([]byte, string) {
	raw := []byte("fixture-png-bytes")
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestTextImageFixtureRoundTripKeepsOriginalBytes(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := fixtureJob(t, s, textimage.StatusQuoted)
	raw, sum := fixtureBytes()
	got, already, err := s.RegisterTextImageFixture(ctx, TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "fixture", SHA256: sum, MediaType: "image/png", Bytes: raw,
	})
	if err != nil || already || got.JobStatus != textimage.StatusCompleted || got.Origin != textimage.OriginFixture {
		t.Fatalf("登记夹具: already=%v err=%v job=%+v", already, err, got)
	}
	if got.InputVersion != "fp-fixture" || got.OutputSHA256 != sum || got.OutputAssetID == "" || got.BillingPassed || got.Charged || got.ProductionAuthorized || got.SubjectProtected {
		t.Fatalf("夹具登记写成了计费或丢了版本: %+v", got)
	}
	media, body, err := s.OpenTextImageDownload(ctx, "ten", "prj", got.ID)
	if err != nil || media != "image/png" || string(body) != string(raw) {
		t.Fatalf("下载夹具: %s %q %v", media, body, err)
	}
	lateRaw := []byte("fixture-png-bytes-late")
	lateSum := sha256.Sum256(lateRaw)
	again, already, err := s.RegisterTextImageFixture(ctx, TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "fixture", SHA256: hex.EncodeToString(lateSum[:]), MediaType: "image/png", Bytes: lateRaw,
	})
	if err != nil || !already || again.OutputAssetID != got.OutputAssetID || again.OutputSHA256 != sum {
		t.Fatalf("晚到字节不能换掉原资产: already=%v err=%v %+v", already, err, again)
	}
	_, body, err = s.OpenTextImageDownload(ctx, "ten", "prj", got.ID)
	if err != nil || string(body) != string(raw) {
		t.Fatalf("原字节被覆盖: %q %v", body, err)
	}
	if _, _, err := s.OpenTextImageDownload(ctx, "other", "prj", got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("别的租户必须不可见: %v", err)
	}
}

func TestTextImageFixtureCannotReviveFailure(t *testing.T) {
	s := openTest(t)
	job := fixtureJob(t, s, textimage.StatusFailed)
	raw, sum := fixtureBytes()
	_, _, err := s.RegisterTextImageFixture(context.Background(), TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "fixture", SHA256: sum, MediaType: "image/png", Bytes: raw,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("失败记录不能改成夹具完成: %v", err)
	}
}

func TestTextImageFixtureTamperIsNotDownloadable(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := fixtureJob(t, s, textimage.StatusQuoted)
	raw, sum := fixtureBytes()
	got, _, err := s.RegisterTextImageFixture(ctx, TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "fixture", SHA256: sum, MediaType: "image/png", Bytes: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE text_image_blobs SET content=? WHERE asset_id=?`, []byte("fixture-tampered"), got.OutputAssetID); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.OpenTextImageDownload(ctx, "ten", "prj", got.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("哈希不符必须拒绝下载: %v", err)
	}
}

func TestTextImageSelectKeepsChosenVersion(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := fixtureJob(t, s, textimage.StatusQuoted)
	raw, sum := fixtureBytes()
	got, _, err := s.RegisterTextImageFixture(ctx, TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "fixture", SHA256: sum, MediaType: "image/png", Bytes: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.SelectTextImageVersion(ctx, "ten", "prj", got.ID)
	if err != nil || selected.SelectedVersion == "" || selected.SelectedVersion != selected.ResultVersion {
		t.Fatalf("选定版本: %v %+v", err, selected)
	}
	again, err := s.SelectTextImageVersion(ctx, "ten", "prj", got.ID)
	if err != nil || again.SelectedVersion != selected.SelectedVersion {
		t.Fatalf("重复选定应保持原版本: %v %+v", err, again)
	}
	quoted, err := s.InsertTextImageJob(ctx, TextImageJob{
		TenantID: "ten", ProjectID: "prj", Prompt: "还没登记",
		Fingerprint: "fp-quoted", QuoteStatus: textimage.QuoteConfirmed, BillingLabel: "配置缺失",
		JobStatus: textimage.StatusQuoted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectTextImageVersion(ctx, "ten", "prj", quoted.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("未登记的任务不能选定: %v", err)
	}
}

func TestTextImageServerResultKeepsFirstBytes(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	job := fixtureJob(t, s, textimage.StatusUnknown)
	raw := []byte("server-png-bytes")
	got, already, err := s.CompleteTextImageServer(ctx, TextImageServerResult{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID, AssetID: "asset_real",
		MediaType: "image/png", Bytes: raw,
	})
	if err != nil || already || got.JobStatus != textimage.StatusCompleted || got.Origin != textimage.OriginServer || got.OutputAssetID != "asset_real" {
		t.Fatalf("服务端任务应写成完成: already=%v err=%v %+v", already, err, got)
	}
	if got.BillingPassed || got.Charged || got.ProductionAuthorized || got.SubjectProtected || got.OutputSHA256 == "" {
		t.Fatalf("完成不能写成计费或丢掉哈希: %+v", got)
	}
	media, body, err := s.OpenTextImageDownload(ctx, "ten", "prj", got.ID)
	if err != nil || media != "image/png" || string(body) != string(raw) {
		t.Fatalf("下载服务端字节: %s %q %v", media, body, err)
	}
	late := []byte("server-png-bytes-late")
	again, already, err := s.CompleteTextImageServer(ctx, TextImageServerResult{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID, AssetID: "asset_late",
		MediaType: "image/png", Bytes: late,
	})
	if err != nil || !already || again.OutputAssetID != "asset_real" {
		t.Fatalf("晚到资产不能覆盖: already=%v err=%v %+v", already, err, again)
	}
	_, body, err = s.OpenTextImageDownload(ctx, "ten", "prj", got.ID)
	if err != nil || string(body) != string(raw) {
		t.Fatalf("原字节被覆盖: %q %v", body, err)
	}
	selected, err := s.SelectTextImageVersion(ctx, "ten", "prj", got.ID)
	if err != nil || selected.SelectedVersion != "asset_real" {
		t.Fatalf("服务端版本应可选定: %v %+v", err, selected)
	}
	if _, _, err := s.OpenTextImageDownload(ctx, "other", "prj", got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("别的租户必须不可见: %v", err)
	}
}

func insertTextJob(t *testing.T, s *Store, fingerprint, status string) TextImageJob {
	t.Helper()
	job, err := s.InsertTextImageJob(context.Background(), TextImageJob{
		TenantID: "ten", ProjectID: "prj", Prompt: fingerprint,
		Fingerprint: fingerprint, QuoteStatus: textimage.QuoteConfirmed, BillingLabel: "配置缺失",
		JobStatus: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestTextImageServerResultRejectsFailureAndGenericUpdate(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	failed := insertTextJob(t, s, "fp-server-failed", textimage.StatusFailed)
	_, _, err := s.CompleteTextImageServer(ctx, TextImageServerResult{
		TenantID: "ten", ProjectID: "prj", JobID: failed.ID, AssetID: "asset_real",
		MediaType: "image/png", Bytes: []byte("server-png-bytes"),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("失败记录不能改成服务端完成: %v", err)
	}
	quoted := insertTextJob(t, s, "fp-server-quoted", textimage.StatusQuoted)
	_, _, err = s.CompleteTextImageServer(ctx, TextImageServerResult{
		TenantID: "ten", ProjectID: "prj", JobID: quoted.ID, AssetID: "asset_real",
		MediaType: "image/png", Bytes: []byte("server-png-bytes"),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("未提交的报价不能直接完成: %v", err)
	}
	unknown := insertTextJob(t, s, "fp-server-unknown", textimage.StatusUnknown)
	unknown.JobStatus = textimage.StatusCompleted
	unknown.OutputAssetID = "filled-output"
	if err := s.UpdateTextImageJob(ctx, unknown); !errors.Is(err, ErrConflict) {
		t.Fatalf("通用更新仍不能写成完成: %v", err)
	}
	_, _, err = s.CompleteTextImageServer(ctx, TextImageServerResult{
		TenantID: "ten", ProjectID: "prj", JobID: unknown.ID, AssetID: "",
		MediaType: "image/png", Bytes: []byte("server-png-bytes"),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("空资产必须拒绝: %v", err)
	}
}

func TestTextImageFixtureLabelIsRequired(t *testing.T) {
	s := openTest(t)
	job := fixtureJob(t, s, textimage.StatusQuoted)
	raw, sum := fixtureBytes()
	_, _, err := s.RegisterTextImageFixture(context.Background(), TextImageFixture{
		TenantID: "ten", ProjectID: "prj", JobID: job.ID,
		FixtureLabel: "sample", SHA256: sum, MediaType: "image/png", Bytes: raw,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("数据未标明 fixture 必须拒绝: %v", err)
	}
}
