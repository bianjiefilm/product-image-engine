package imagemodel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGenerateDecodesPNGAndKeepsLoopbackUnlive(t *testing.T) {
	pngBytes := solidPNG(t, color.NRGBA{R: 11, G: 22, B: 33, A: 255})
	var seenAuth, seenPrompt, seenModel string
	var calls int
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		seenAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seenPrompt, _ = body["prompt"].(string)
		seenModel, _ = body["model"].(string)
		raw, _ := json.Marshal(map[string]any{
			"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(raw))),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	got, err := Generate(context.Background(), client, Request{
		Endpoint:   "http://127.0.0.1:9/v1/images/generations",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "白色陶瓷杯",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || seenAuth != "Bearer secret-model-key" || seenPrompt != "白色陶瓷杯" || seenModel != "qwen-image-2.0-pro" {
		t.Fatalf("请求没有把提示词和模型交给端点: calls=%d auth=%q prompt=%q model=%q", calls, seenAuth, seenPrompt, seenModel)
	}
	if len(got.Bytes) == 0 || got.MediaType != "image/png" || got.HostLive || got.BillingPassed {
		t.Fatalf("回环地址的图片不能算供应商出图，也不能标成已扣费: %+v n=%d", got, len(got.Bytes))
	}
	if _, err := png.Decode(strings.NewReader(string(got.Bytes))); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateFollowsResultURLOnPublicHost(t *testing.T) {
	pngBytes := solidPNG(t, color.NRGBA{R: 4, G: 5, B: 6, A: 255})
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "images.example" {
			raw := `{"data":[{"url":"https://cdn.example/out.png"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(raw)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}
		if r.URL.String() != "https://cdn.example/out.png" {
			t.Fatalf("只应回捞响应里的地址, got %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("回捞图片地址不能带上模型凭证")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(pngBytes))),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	got, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "浅灰棚拍",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HostLive || got.BillingPassed || got.MediaType != "image/png" || len(got.SHA256) != 64 {
		t.Fatalf("公网端点返回的图片应可解码且未扣费: %+v", got)
	}
}

func TestGenerateMissingCredentialDoesNotCall(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Fatal("缺凭证不应发出请求")
		return nil, nil
	})}
	_, err := Generate(context.Background(), client, Request{
		Endpoint: "https://images.example/v1/images/generations",
		Model:    "qwen-image-2.0-pro",
		Prompt:   "杯子",
	})
	if err == nil || !strings.Contains(err.Error(), "配置缺失") {
		t.Fatalf("缺凭证应是配置缺失, got %v", err)
	}
}

func TestGenerateRejectsUndecodableBody(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw := `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("not-an-image")) + `"}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(raw)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	_, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1/images/generations",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "杯子",
	})
	if err == nil || !strings.Contains(err.Error(), "图片无法解码") {
		t.Fatalf("非图片应拒绝, got %v", err)
	}
}

type roundFunc func(*http.Request) (*http.Response, error)

func (f roundFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func roundTrip(fn func(*http.Request) (*http.Response, error)) http.RoundTripper {
	return roundFunc(fn)
}

func solidPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf strings.Builder
	// png.Encode needs io.Writer; strings.Builder works.
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return []byte(buf.String())
}
