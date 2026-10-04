package revision

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

func TestClickAndNaturalLanguageShareOneIntent(t *testing.T) {
	cases := []struct {
		text       string
		action     string
		target     string
		downstream string
	}{
		{"背景简单一点", ActionSimplifyBackground, "", ""},
		{"换成户外", ActionOutdoor, "", ""},
		{"保留商品，只调亮", ActionBrightenKeepProduct, "", ""},
		{"保留商品,只调亮", ActionBrightenKeepProduct, "", ""},
		{"回到V1", ActionRollback, "V1", ""},
		{"回到 V2", ActionRollback, "V2", ""},
		{"采用这个版本", ActionAdopt, "", ""},
		{"送数字人", ActionSendDownstream, "", DownstreamDigitalHuman},
		{"送AiCut", ActionSendDownstream, "", DownstreamAICut},
		{"送矩阵", ActionSendDownstream, "", DownstreamMatrix},
	}
	for _, tc := range cases {
		nl, err := ParseNaturalLanguage(tc.text)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.text, err)
		}
		click, err := FromClick(tc.action, tc.target, tc.downstream)
		if err != nil {
			t.Fatalf("click %s: %v", tc.action, err)
		}
		if !SameIntent(nl, click) {
			t.Fatalf("intent diverged for %q:\nnl=%+v\nclick=%+v", tc.text, nl, click)
		}
		if nl.Source == click.Source {
			t.Fatalf("source must record which channel produced %q", tc.text)
		}
		if !nl.PreserveSubject || nl.Schema != Schema {
			t.Fatalf("locked intent must preserve the subject: %+v", nl)
		}
		nlCost, err := IncrementalCostCents(nl)
		if err != nil {
			t.Fatal(err)
		}
		clickCost, err := IncrementalCostCents(click)
		if err != nil {
			t.Fatal(err)
		}
		if nlCost != clickCost {
			t.Fatalf("cost diverged for %q: %d vs %d", tc.text, nlCost, clickCost)
		}
	}
}

func TestIncrementalCostIsRecomputableIntegerFen(t *testing.T) {
	bg, _ := ParseNaturalLanguage("背景简单一点")
	light, _ := ParseNaturalLanguage("保留商品，只调亮")
	back, _ := ParseNaturalLanguage("回到V1")
	send, _ := ParseNaturalLanguage("送矩阵")
	adopt, _ := ParseNaturalLanguage("采用这个版本")
	outdoor, _ := ParseNaturalLanguage("换成户外")

	checks := []struct {
		name   string
		intent Intent
		want   int64
	}{
		{"background", bg, 40},
		{"outdoor", outdoor, 40},
		{"light", light, 20},
		{"rollback", back, 0},
		{"downstream", send, 0},
		{"adopt", adopt, 0},
	}
	for _, tc := range checks {
		first, err := IncrementalCostCents(tc.intent)
		second, err2 := IncrementalCostCents(tc.intent)
		if err != nil || err2 != nil || first != tc.want || second != first {
			t.Fatalf("%s cost = %d/%d err=%v/%v, want stable %d", tc.name, first, second, err, err2, tc.want)
		}
	}
	bg.PreserveSubject = false
	if _, err := IncrementalCostCents(bg); !errors.Is(err, ErrPreserveSubject) {
		t.Fatalf("full redraw must not be priced, got %v", err)
	}
}

func TestUnrecognizedDoesNotRedraw(t *testing.T) {
	intent, err := ParseNaturalLanguage("把整张图重新画一遍")
	if err != nil {
		t.Fatal(err)
	}
	if intent.Action != ActionUnrecognized || !intent.PreserveSubject {
		t.Fatalf("unrecognized must stay locked: %+v", intent)
	}
	_, err = Execute(ExecuteRequest{Intent: intent, Original: mustPNG(t, 4, 4, color.NRGBA{255, 0, 0, 255}), Mask: maskCenter(t, 4, 4)})
	if !errors.Is(err, ErrUnrecognized) {
		t.Fatalf("got %v", err)
	}
}

func TestPartialRevisionSkipsExpensiveStepsAndKeepsSubjectPixels(t *testing.T) {
	intent, err := ParseNaturalLanguage("背景简单一点")
	if err != nil {
		t.Fatal(err)
	}
	original := subjectFixture(t)
	mask := maskCenter(t, 8, 8)
	hook := &countingRedraw{}
	got, err := Execute(ExecuteRequest{
		Intent: intent, Provider: Provider{PartialEdit: false}, Acknowledged: true,
		Original: original, Mask: mask, FullRedraw: hook,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hook.calls != 0 || got.SupplierCalls != 0 || got.Plan.CallsSupplier {
		t.Fatalf("supplier or full redraw ran: hook=%d supplier=%d plan=%v", hook.calls, got.SupplierCalls, got.Plan.CallsSupplier)
	}
	for _, step := range ExpensiveSteps {
		for _, ran := range got.StepsRun {
			if ran == step {
				t.Fatalf("expensive step ran: %s in %v", step, got.StepsRun)
			}
		}
	}
	if !containsAll(got.Plan.Skipped, ExpensiveSteps) {
		t.Fatalf("plan skipped = %v", got.Plan.Skipped)
	}
	if got.Origin != bgreplace.OriginSubjectLock || !got.SubjectPreserved {
		t.Fatalf("origin/subject = %s %v", got.Origin, got.SubjectPreserved)
	}
	assertSubjectHeld(t, original, mask, got.PNG, color.NRGBA{220, 220, 220, 255})
}

func TestImpactIsExplainedBeforeExecution(t *testing.T) {
	intent, _ := FromClick(ActionOutdoor, "", "")
	original := subjectFixture(t)
	mask := maskCenter(t, 8, 8)
	hook := &countingRedraw{}
	plan, err := PlanRevision(intent, Provider{PartialEdit: false})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresAcknowledgement || plan.ImpactNotice == "" || plan.CallsSupplier {
		t.Fatalf("plan = %+v", plan)
	}
	_, err = Execute(ExecuteRequest{
		Intent: intent, Provider: Provider{PartialEdit: false}, Acknowledged: false,
		Original: original, Mask: mask, FullRedraw: hook,
	})
	if !errors.Is(err, ErrImpactUnacknowledged) || hook.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, hook.calls)
	}
	ready, err := PlanRevision(intent, Provider{PartialEdit: true})
	if err != nil || ready.RequiresAcknowledgement || ready.CallsSupplier || ready.SupplierNote == "" {
		t.Fatalf("partial provider still must not claim a real supplier: %+v %v", ready, err)
	}
}

func TestPreserveSubjectRejectsSilentFullRedraw(t *testing.T) {
	intent, _ := FromClick(ActionSimplifyBackground, "", "")
	intent.PreserveSubject = false
	hook := &countingRedraw{}
	_, err := Execute(ExecuteRequest{
		Intent: intent, Provider: Provider{PartialEdit: true}, Acknowledged: true,
		Original: subjectFixture(t), Mask: maskCenter(t, 8, 8), FullRedraw: hook,
	})
	if !errors.Is(err, ErrPreserveSubject) || hook.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, hook.calls)
	}
}

type countingRedraw struct{ calls int }

func (c *countingRedraw) FullRedraw(Intent, []byte) ([]byte, error) {
	c.calls++
	return nil, errors.New("full redraw was called")
}

func subjectFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x >= 2 && x < 6 && y >= 2 && y < 6 {
				img.SetNRGBA(x, y, color.NRGBA{180, 20, 20, 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{10, 10, 10, 255})
			}
		}
	}
	return encodePNG(t, img)
}

func maskCenter(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= 2 && x < w-2 && y >= 2 && y < h-2 {
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 0})
			}
		}
	}
	return encodePNG(t, img)
}

func mustPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return encodePNG(t, img)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertSubjectHeld(t *testing.T, original, mask, result []byte, plate color.NRGBA) {
	t.Helper()
	src := decode(t, original)
	matte := decode(t, mask)
	got := decode(t, result)
	protected := 0
	changed := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if matte.NRGBAAt(x, y).A >= 128 {
				protected++
				if src.NRGBAAt(x, y) != got.NRGBAAt(x, y) {
					t.Fatalf("subject pixel changed at %d,%d", x, y)
				}
				continue
			}
			if got.NRGBAAt(x, y) != plate {
				t.Fatalf("background pixel = %+v, want plate %+v", got.NRGBAAt(x, y), plate)
			}
			if got.NRGBAAt(x, y) != src.NRGBAAt(x, y) {
				changed++
			}
		}
	}
	if protected == 0 || changed == 0 {
		t.Fatal("fixture did not lock a subject and change the background")
	}
}

func decode(t *testing.T, raw []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	switch v := img.(type) {
	case *image.NRGBA:
		return v
	default:
		b := v.Bounds()
		out := image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, b8, a := v.At(x, y).RGBA()
				out.SetNRGBA(x, y, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b8 >> 8), uint8(a >> 8)})
			}
		}
		return out
	}
}

func containsAll(got, want []string) bool {
	set := map[string]bool{}
	for _, s := range got {
		set[s] = true
	}
	for _, s := range want {
		if !set[s] {
			return false
		}
	}
	return true
}
