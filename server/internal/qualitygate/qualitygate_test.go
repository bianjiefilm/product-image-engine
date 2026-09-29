package qualitygate

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestMissingConfigReturnsNilRecord(t *testing.T) {
	cases := []Input{
		{ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown"},
		{TenantID: "  ", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: " ", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "\t", Quality: "unknown", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: " ", Generation: "unknown", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "\n", Fee: "unknown"},
		{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: " "},
	}
	reg := NewRegistry()
	for _, in := range cases {
		rec, err := Build(in)
		if rec != nil || err == nil || err.Error() != "配置缺失" || !errors.Is(err, ErrConfigMissing) {
			t.Fatalf("缺失配置应返回配置缺失和 nil 记录: %+v err=%v", rec, err)
		}
		stored, storedErr := reg.Submit(in)
		if stored != nil || storedErr == nil || storedErr.Error() != "配置缺失" || reg.Checks() != 0 {
			t.Fatalf("缺失配置不得入库: %+v err=%v checks=%d", stored, storedErr, reg.Checks())
		}
	}
}

func TestBannedPhraseDoesNotStore(t *testing.T) {
	cases := []struct {
		name string
		in   Input
	}{
		{"quality", Input{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "质量通过", Generation: "unknown", Fee: "unknown"}},
		{"generation", Input{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "前缀工程通过后缀", Fee: "unknown"}},
		{"fee", Input{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "see Billing PASS"}},
		{"fee-lower", Input{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "billing pass"}},
		{"digest", Input{TenantID: "ten", ProjectID: "prj", Digest: "真实出图完成", Quality: "unknown", Generation: "unknown", Fee: "unknown"}},
		{"tenant", Input{TenantID: "质量通过", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"}},
		{"logical", Input{LogicalID: "工程通过", TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"}},
		{"phrase", Input{TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown", CallerPhrase: "真实出图完成"}},
	}
	reg := NewRegistry()
	for _, tc := range cases {
		rec, err := Build(tc.in)
		if rec != nil || !errors.Is(err, ErrCallerPhrase) || err.Error() != "调用方口径被拒绝" {
			t.Fatalf("%s: got rec=%+v err=%v", tc.name, rec, err)
		}
		assertNoBanned(t, err.Error())
		stored, storedErr := reg.Submit(tc.in)
		if stored != nil || !errors.Is(storedErr, ErrCallerPhrase) || reg.Checks() != 0 {
			t.Fatalf("%s: 拒绝不得入库 rec=%+v err=%v checks=%d", tc.name, stored, storedErr, reg.Checks())
		}
		assertNoBanned(t, storedErr.Error())
	}
	rec, err := Build(Input{Quality: "质量通过"})
	if rec != nil || !errors.Is(err, ErrConfigMissing) || err.Error() != "配置缺失" {
		t.Fatalf("缺标识时优先配置缺失: %+v %v", rec, err)
	}
	assertNoBanned(t, err.Error())
}

func TestUnknownStaysAndCallerCannotFlipFlags(t *testing.T) {
	const want = `{"schema_version":"product-value-check/v1","logical_id":"chk-1","tenant_id":"ten","project_id":"prj","digest":"AbC","quality":"Unknown","generation":"unknown","fee":"not-charged","status":"真实出图未完成","quality_passed":false,"generation_passed":false,"billing_passed":false,"charged":null,"provider_calls":0,"production":"NOT_AUTHORIZED","notice":"此记录不通过产品价值门","labels":["Engineering for the recorder test only","Browser NOT_RUN","Service NOT_VERIFIED","Billing NOT_VERIFIED","Production NOT_AUTHORIZED","Human UNKNOWN"]}`
	charged := "9.99"
	in := Input{
		LogicalID: " chk-1 ", TenantID: " ten ", ProjectID: " prj ", Digest: " AbC ",
		Quality: " Unknown ", Generation: " unknown ", Fee: " not-charged ",
		QualityPassed: true, GenerationPassed: true, BillingPassed: true,
		Charged: &charged, ProviderCalls: 4, Status: "真实出图完成", Production: "AUTHORIZED_BY_CALLER",
		ImageBytes: []byte("NOT-AN-IMAGE"),
	}
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
	if bytes.Contains(a, []byte("NOT-AN-IMAGE")) || bytes.Contains(a, []byte("AUTHORIZED_BY_CALLER")) || bytes.Contains(a, []byte("真实出图完成")) || bytes.Contains(a, []byte(`"quality_passed":true`)) {
		t.Fatalf("调用方的成功口径进了记录: %s", a)
	}
	if first.Quality != "Unknown" || first.Generation != "unknown" || first.Fee != "not-charged" || first.Digest != "AbC" {
		t.Fatalf("未知观测或摘要被改写: %+v", first)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(a, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["charged"]) != "null" || string(raw["provider_calls"]) != "0" || string(raw["quality_passed"]) != "false" {
		t.Fatalf("null 与 false 没有保住: %s", a)
	}
	amp, err := Build(Input{LogicalID: "chk-1", TenantID: "a&b", ProjectID: "prj", Digest: "AbC", Quality: "Unknown", Generation: "unknown", Fee: "not-charged"})
	if err != nil {
		t.Fatal(err)
	}
	ab, err := CanonicalJSON(amp)
	if err != nil || !bytes.Contains(ab, []byte(`"tenant_id":"a&b"`)) || bytes.Contains(ab, []byte(`\u0026`)) {
		t.Fatalf("规范 JSON 不应转义 &: %s err=%v", ab, err)
	}
	derived, err := Build(Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "unknown", Generation: "unknown", Fee: "unknown", LogicalID: "  "})
	if err != nil || len(derived.LogicalID) != 64 {
		t.Fatalf("空编号应稳定派生: %+v %v", derived, err)
	}
	again, err := Build(Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "unknown", Generation: "unknown", Fee: "unknown"})
	if err != nil || again.LogicalID != derived.LogicalID || again.LogicalID != LogicalID(Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-1"}) {
		t.Fatalf("派生编号不稳定: %+v %v", again, err)
	}
	mut := *first
	mut.Labels = append([]string(nil), first.Labels...)
	mut.QualityPassed = true
	out, err := CanonicalJSON(&mut)
	if out != nil || err == nil || err.Error() != "记录保持未完成" {
		t.Fatalf("旗标不能编码成通过: %s %v", out, err)
	}
	assertNoBanned(t, err.Error())
	mut = *first
	mut.Labels = append([]string(nil), first.Labels...)
	mut.Status = "真实出图完成"
	out, err = CanonicalJSON(&mut)
	if out != nil || err == nil || strings.Contains(err.Error(), "真实出图完成") {
		t.Fatalf("禁用短语不能成为编码结果: %s %v", out, err)
	}
	out, err = CanonicalJSON(nil)
	if out != nil || err == nil || err.Error() != "记录保持未完成" {
		t.Fatalf("nil 记录不能编码: %s %v", out, err)
	}
	for _, e := range []error{ErrConfigMissing, ErrCallerPhrase, ErrKeptIncomplete} {
		assertNoBanned(t, e.Error())
	}
}

func TestReplayReturnsSameRecordWithoutSecondCheck(t *testing.T) {
	reg := NewRegistry()
	first, err := reg.Submit(Input{LogicalID: "job-7", TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "unknown", Generation: "unknown", Fee: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	charged := "1"
	second, err := reg.Submit(Input{
		LogicalID: " job-7 ", TenantID: "other", ProjectID: "other", Digest: "other",
		Quality: "质量通过", Generation: "工程通过", Fee: "Billing PASS",
		QualityPassed: true, GenerationPassed: true, BillingPassed: true,
		Charged: &charged, ProviderCalls: 9, Status: "真实出图完成",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Checks() != 1 {
		t.Fatalf("回放不得建立第二次检查: %d", reg.Checks())
	}
	a, err := CanonicalJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(second)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("回放必须仍是第一份未完成记录: %s err=%v", b, err)
	}
	assertHonest(t, second)
	assertNoBanned(t, string(b))
	if second.Quality != "unknown" || second.TenantID != "ten" {
		t.Fatalf("回放改写了第一份记录: %+v", second)
	}
	first.QualityPassed = true
	first.Labels[0] = "changed"
	third, err := reg.Submit(Input{LogicalID: "job-7", TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "unknown", Generation: "unknown", Fee: "unknown"})
	if err != nil || third.QualityPassed || third.Labels[0] != "Engineering for the recorder test only" || reg.Checks() != 1 {
		t.Fatalf("改副本不得污染库内记录: %+v checks=%d err=%v", third, reg.Checks(), err)
	}
	missing, err := reg.Submit(Input{LogicalID: "job-7", TenantID: "ten"})
	if missing != nil || err == nil || err.Error() != "配置缺失" || reg.Checks() != 1 {
		t.Fatalf("缺字段的回放不得返回已存记录: %+v %v checks=%d", missing, err, reg.Checks())
	}
}

func TestReplayDerivedIDDoesNotCreateSecondCheck(t *testing.T) {
	reg := NewRegistry()
	in := Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "unknown", Generation: "unknown", Fee: "unknown"}
	first, err := reg.Submit(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Submit(Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-1", Quality: "observed", Generation: "observed", Fee: "observed"})
	if err != nil {
		t.Fatal(err)
	}
	if reg.Checks() != 1 || second.Quality != "unknown" {
		t.Fatalf("同一派生编号不得改写或加检: checks=%d rec=%+v", reg.Checks(), second)
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
	other, err := reg.Submit(Input{TenantID: "ten", ProjectID: "prj", Digest: "digest-2", Quality: "unknown", Generation: "unknown", Fee: "unknown"})
	if err != nil || reg.Checks() != 2 || other.LogicalID == first.LogicalID {
		t.Fatalf("不同摘要应是另一次检查: checks=%d %+v %v", reg.Checks(), other, err)
	}
}

func TestConcurrentReplayStoresOneCheck(t *testing.T) {
	reg := NewRegistry()
	in := Input{LogicalID: "same", TenantID: "ten", ProjectID: "prj", Digest: "d", Quality: "unknown", Generation: "unknown", Fee: "unknown"}
	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := reg.Submit(in)
			if err != nil || rec == nil || rec.QualityPassed || rec.Status != "真实出图未完成" {
				if err == nil {
					err = errors.New("concurrent record was not incomplete")
				}
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if reg.Checks() != 1 {
		t.Fatalf("并发回放建立了多次检查: %d", reg.Checks())
	}
}

func TestMutatedSuccessShapeIsNotEncoded(t *testing.T) {
	base, err := Build(Input{
		LogicalID: "chk-1", TenantID: "ten", ProjectID: "prj", Digest: "d",
		Quality: "unknown", Generation: "unknown", Fee: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	charged := "1"
	cases := []struct {
		name string
		edit func(*Record)
	}{
		{"generation", func(r *Record) { r.GenerationPassed = true }},
		{"billing", func(r *Record) { r.BillingPassed = true }},
		{"calls", func(r *Record) { r.ProviderCalls = 1 }},
		{"charged", func(r *Record) { r.Charged = &charged }},
		{"production", func(r *Record) { r.Production = "AUTHORIZED_BY_CALLER" }},
		{"notice", func(r *Record) { r.Notice = "changed" }},
		{"version", func(r *Record) { r.SchemaVersion = "other" }},
		{"quality-empty", func(r *Record) { r.Quality = "" }},
	}
	for _, tc := range cases {
		rec := base.clone()
		tc.edit(rec)
		out, err := CanonicalJSON(rec)
		if out != nil || err == nil || err.Error() != "记录保持未完成" {
			t.Fatalf("%s: 成功形态被编码 %s %v", tc.name, out, err)
		}
		assertNoBanned(t, err.Error())
	}
}

func TestProductionSourcesAvoidForbiddenImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"bytes": true, "crypto/sha256": true, "encoding/hex": true, "encoding/json": true,
		"errors": true, "strings": true, "sync": true,
	}
	saw := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		saw = true
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed[path] {
				t.Fatalf("%s 导入了未允许的包 %s", name, path)
			}
		}
	}
	if !saw {
		t.Fatal("没有生产代码")
	}
}

func assertHonest(t *testing.T, rec *Record) {
	t.Helper()
	if rec == nil || rec.Status != "真实出图未完成" || rec.QualityPassed || rec.GenerationPassed || rec.BillingPassed ||
		rec.Charged != nil || rec.ProviderCalls != 0 || rec.Production != "NOT_AUTHORIZED" ||
		rec.Notice != "此记录不通过产品价值门" || rec.SchemaVersion != "product-value-check/v1" {
		t.Fatalf("记录不是未完成的产品价值检查: %+v", rec)
	}
	want := []string{
		"Engineering for the recorder test only",
		"Browser NOT_RUN",
		"Service NOT_VERIFIED",
		"Billing NOT_VERIFIED",
		"Production NOT_AUTHORIZED",
		"Human UNKNOWN",
	}
	if !reflect.DeepEqual(rec.Labels, want) || !reflect.DeepEqual(GateLabels(), want) {
		t.Fatalf("六枚标签不对: %#v", rec.Labels)
	}
}

func assertNoBanned(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, phrase := range []string{"工程通过", "质量通过", "真实出图完成"} {
		if strings.Contains(text, phrase) {
			t.Fatalf("产出含禁用短语 %q: %s", phrase, text)
		}
	}
	if strings.Contains(lower, "billing pass") {
		t.Fatalf("产出含禁用短语 Billing PASS: %s", text)
	}
}
