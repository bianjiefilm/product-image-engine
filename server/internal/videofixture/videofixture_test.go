package videofixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMissingConfigReturnsNilDocument(t *testing.T) {
	cases := []Input{
		{TenantID: "ten", ProjectID: "prj"},
		{TenantID: "ten", ImageDigest: "digest-1"},
		{ProjectID: "prj", ImageDigest: "digest-1"},
		{TenantID: "  ", ProjectID: "prj", ImageDigest: "digest-1"},
		{TenantID: "ten", ProjectID: " ", ImageDigest: "digest-1"},
		{TenantID: "ten", ProjectID: "prj", ImageDigest: "\t"},
	}
	reg := NewRegistry()
	for _, in := range cases {
		doc, err := Build(in)
		if doc != nil || err == nil || err.Error() != "配置缺失" || !errors.Is(err, ErrConfigMissing) {
			t.Fatalf("缺失配置应返回配置缺失和 nil 文档: %+v err=%v", doc, err)
		}
		stored, storedErr := reg.Submit(in)
		if stored != nil || storedErr == nil || storedErr.Error() != "配置缺失" || reg.Jobs() != 0 {
			t.Fatalf("缺失配置不得入库: %+v err=%v jobs=%d", stored, storedErr, reg.Jobs())
		}
	}
}

func TestRejectedInputDoesNotBecomeSuccess(t *testing.T) {
	zero, neg := 0, -1
	cases := []struct {
		name string
		in   Input
		want error
	}{
		{"still", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", Still: true}, ErrStillImage},
		{"jpeg", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", MediaType: "image/jpeg"}, ErrStillImage},
		{"png", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", MediaType: " IMAGE/PNG "}, ErrStillImage},
		{"url", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", PlaceholderURL: "https://example.com/v.mp4"}, ErrPlaceholderURL},
		{"placeholder", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", PlaceholderURL: "placeholder"}, ErrPlaceholderURL},
		{"blank", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", PlaceholderURL: "about:blank"}, ErrPlaceholderURL},
		{"zero", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", ByteCount: &zero}, ErrEmptyByteClaim},
		{"neg", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", ByteCount: &neg}, ErrEmptyByteClaim},
		{"empty-bytes", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", ClaimedBytes: []byte{}}, ErrEmptyByteClaim},
		{"phrase-cn", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "图生视频成功"}, ErrSuccessPhrase},
		{"phrase-play", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "可播放"}, ErrSuccessPhrase},
		{"phrase-bill", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "Billing PASS"}, ErrSuccessPhrase},
		{"phrase-draw", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "真实出图完成"}, ErrSuccessPhrase},
		{"phrase-en", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "the video succeeded"}, ErrSuccessPhrase},
		{"phrase-completed", Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "d", CallerPhrase: "completed"}, ErrSuccessPhrase},
		{"id-banned", Input{TenantID: "图生视频成功", ProjectID: "prj", ImageDigest: "d"}, ErrSuccessPhrase},
	}
	reg := NewRegistry()
	for _, tc := range cases {
		doc, err := Build(tc.in)
		if doc != nil || !errors.Is(err, tc.want) {
			t.Fatalf("%s: got doc=%+v err=%v", tc.name, doc, err)
		}
		assertNoBanned(t, err.Error())
		stored, storedErr := reg.Submit(tc.in)
		if stored != nil || !errors.Is(storedErr, tc.want) || reg.Jobs() != 0 {
			t.Fatalf("%s: 拒绝不得入库 doc=%+v err=%v jobs=%d", tc.name, stored, storedErr, reg.Jobs())
		}
		assertNoBanned(t, storedErr.Error())
	}
	doc, err := Build(Input{ProjectID: "prj", Still: true, CallerPhrase: "图生视频成功"})
	if doc != nil || !errors.Is(err, ErrConfigMissing) || err.Error() != "配置缺失" {
		t.Fatalf("缺标识时优先配置缺失: %+v %v", doc, err)
	}
	assertNoBanned(t, err.Error())
}

func TestUnverifiedDocumentCanonicalJSON(t *testing.T) {
	const want = `{"schema_version":"image-to-video-fixture/v1","request_id":"req-1","tenant_id":"ten","project_id":"prj","image_digest":"digest-1","status":"真实视频未验证","show_video":false,"play_still":false,"video_url":"","provider_calls":0,"generation_triggered":false,"billing_passed":false,"charged":null,"production_status":"NOT_AUTHORIZED","real_video":false,"notice":"夹具不是真实视频","labels":["Engineering","Browser NOT_RUN","Service NOT_VERIFIED","Billing NOT_VERIFIED","Production NOT_AUTHORIZED","Human UNKNOWN"]}`
	in := Input{RequestID: "req-1", TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"}
	first, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	a, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(second)
	if err != nil || !bytes.Equal(a, b) || string(a) != want {
		t.Fatalf("同一输入的规范 JSON 不稳定:\n%s\n%s", a, b)
	}
	assertHonest(t, first)
	assertNoBanned(t, string(a))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(a, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["charged"]) != "null" || string(raw["video_url"]) != `""` || string(raw["provider_calls"]) != "0" {
		t.Fatalf("charged、地址或供应商计数不对: %s", a)
	}
	trimmed, err := Build(Input{RequestID: "req-1", TenantID: " ten ", ProjectID: " prj ", ImageDigest: " digest-1 "})
	if err != nil {
		t.Fatal(err)
	}
	tb, err := CanonicalJSON(trimmed)
	if err != nil || !bytes.Equal(tb, a) {
		t.Fatalf("空白应被折掉: %s", tb)
	}
	amp, err := Build(Input{RequestID: "req-1", TenantID: "a&b", ProjectID: "prj", ImageDigest: "digest-1"})
	if err != nil {
		t.Fatal(err)
	}
	ab, err := CanonicalJSON(amp)
	if err != nil || !bytes.Contains(ab, []byte(`"tenant_id":"a&b"`)) || bytes.Contains(ab, []byte(`\u0026`)) {
		t.Fatalf("规范 JSON 不应转义 &: %s err=%v", ab, err)
	}
	derived, err := Build(Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1", RequestID: "  "})
	if err != nil || len(derived.RequestID) != 64 || derived.RequestID != LogicalRequestID(Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"}) {
		t.Fatalf("空编号应稳定派生: %+v %v", derived, err)
	}
	again, _ := Build(Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"})
	if again.RequestID != derived.RequestID {
		t.Fatal("同一输入的派生编号应相同")
	}
	n := 12
	claimed, err := Build(Input{
		RequestID: "req-b", TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1",
		MediaType: "video/mp4", ByteCount: &n, ClaimedBytes: []byte("NOT-A-VIDEO"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := CanonicalJSON(claimed)
	if err != nil || bytes.Contains(cb, []byte("NOT-A-VIDEO")) {
		t.Fatalf("字节声明不得进文档: %s %v", cb, err)
	}
	assertHonest(t, claimed)
	blankURL, err := Build(Input{RequestID: "req-1", TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1", PlaceholderURL: "   "})
	if err != nil || blankURL.VideoURL != "" {
		t.Fatalf("空白地址不是占位视频: %+v %v", blankURL, err)
	}
	mut := *first
	mut.Labels = append([]string(nil), first.Labels...)
	mut.ShowVideo = true
	if out, err := CanonicalJSON(&mut); out != nil || err == nil || err.Error() != "夹具保持未验证" {
		t.Fatalf("画面开关不能编码成成功: %s %v", out, err)
	}
	mut = *first
	mut.Labels = append([]string(nil), first.Labels...)
	mut.ProviderCalls = 1
	if out, err := CanonicalJSON(&mut); out != nil || err == nil || err.Error() != "夹具保持未验证" {
		t.Fatalf("供应商计数不能编码成成功: %s %v", out, err)
	}
	mut = *first
	mut.Labels = append([]string(nil), first.Labels...)
	mut.Status = "图生视频成功"
	out, err := CanonicalJSON(&mut)
	if out != nil || err == nil || strings.Contains(err.Error(), "图生视频成功") {
		t.Fatalf("禁用短语不能成为编码结果: %s %v", out, err)
	}
	if out, err := CanonicalJSON(nil); out != nil || err == nil || err.Error() != "夹具保持未验证" {
		t.Fatalf("nil 文档不能编码: %s %v", out, err)
	}
}

func TestReplayReturnsSameDocumentWithoutSecondJob(t *testing.T) {
	reg := NewRegistry()
	first, err := reg.Submit(Input{RequestID: "job-7", TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Submit(Input{
		RequestID: " job-7 ", TenantID: "other", ProjectID: "other", ImageDigest: "other",
		Still: true, PlaceholderURL: "https://example.com/x", CallerPhrase: "图生视频成功",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Jobs() != 1 {
		t.Fatalf("回放不得建立第二个任务: %d", reg.Jobs())
	}
	a, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("回放必须仍是第一份未验证文档: %s err=%v", b, err)
	}
	assertHonest(t, second)
	assertNoBanned(t, string(b))
	first.ShowVideo = true
	first.Labels[0] = "changed"
	third, err := reg.Submit(Input{RequestID: "job-7", TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"})
	if err != nil || third.ShowVideo || third.Labels[0] != "Engineering" || reg.Jobs() != 1 {
		t.Fatalf("改副本不得污染库内文档: %+v jobs=%d err=%v", third, reg.Jobs(), err)
	}
	assertHonest(t, third)
}

func TestReplayDerivedIDDoesNotCreateSecondJob(t *testing.T) {
	reg := NewRegistry()
	in := Input{TenantID: "ten", ProjectID: "prj", ImageDigest: "digest-1"}
	first, err := reg.Submit(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Submit(in)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Jobs() != 1 {
		t.Fatalf("同一输入不得建立第二个任务: %d", reg.Jobs())
	}
	a, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("派生编号回放 JSON 不同: %s vs %s err=%v", a, b, err)
	}
	assertHonest(t, second)
	assertNoBanned(t, string(a))
}

func assertHonest(t *testing.T, doc *Document) {
	t.Helper()
	if doc == nil || doc.Status != "真实视频未验证" || doc.ShowVideo || doc.PlayStill || doc.VideoURL != "" ||
		doc.ProviderCalls != 0 || doc.GenerationTriggered || doc.BillingPassed || doc.Charged != nil ||
		doc.ProductionStatus != "NOT_AUTHORIZED" || doc.RealVideo || doc.Notice != "夹具不是真实视频" ||
		doc.SchemaVersion != "image-to-video-fixture/v1" {
		t.Fatalf("文档不是未验证夹具: %+v", doc)
	}
	want := []string{"Engineering", "Browser NOT_RUN", "Service NOT_VERIFIED", "Billing NOT_VERIFIED", "Production NOT_AUTHORIZED", "Human UNKNOWN"}
	if !reflect.DeepEqual(doc.Labels, want) || !reflect.DeepEqual(GateLabels(), want) {
		t.Fatalf("六枚标签不对: %#v", doc.Labels)
	}
}

func assertNoBanned(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, phrase := range []string{"图生视频成功", "可播放", "真实出图完成"} {
		if strings.Contains(text, phrase) {
			t.Fatalf("产出含禁用短语 %q: %s", phrase, text)
		}
	}
	if strings.Contains(lower, "billing pass") {
		t.Fatalf("产出含禁用短语 Billing PASS: %s", text)
	}
}
