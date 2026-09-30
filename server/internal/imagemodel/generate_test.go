package imagemodel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
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
		if _, ok := body["image"]; ok {
			t.Fatal("没有参考图时 JSON 不带 image")
		}
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
	if err == nil || err.Error() != "配置缺失" {
		t.Fatalf("缺凭证应是配置缺失, got %v", err)
	}
}

func TestGenerateReferenceDataURLAndStripsCredentialOnOtherHost(t *testing.T) {
	ref := solidPNG(t, color.NRGBA{R: 9, G: 8, B: 7, A: 255})
	jpegRef := solidJPEG(t)
	out := solidPNG(t, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	var posts int
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posts++
			if r.URL.Host != "images.example" || !strings.HasSuffix(r.URL.Path, "/images/generations") {
				t.Fatalf("应 POST 生成端点, got %s", r.URL)
			}
			if r.Header.Get("Authorization") != "Bearer secret-model-key" {
				t.Fatal("生成端点应带 Bearer 凭证")
			}
			raw, _ := io.ReadAll(r.Body)
			if bytes.Contains(raw, []byte("secret-model-key")) {
				t.Fatal("凭证不能出现在 body")
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			images, _ := body["image"].([]any)
			if len(images) != 2 {
				t.Fatalf("image 应为 data URL 数组: %#v", body["image"])
			}
			pngURL, _ := images[0].(string)
			jpegURL, _ := images[1].(string)
			if !strings.HasPrefix(pngURL, "data:image/png;base64,") || !strings.HasPrefix(jpegURL, "data:image/jpeg;base64,") {
				t.Fatalf("媒体类型应沿用 sniff: %s %s", pngURL, jpegURL)
			}
			gotPNG, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pngURL, "data:image/png;base64,"))
			if err != nil || !bytes.Equal(gotPNG, ref) {
				t.Fatal("png 参考图不一致")
			}
			gotJPEG, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(jpegURL, "data:image/jpeg;base64,"))
			if err != nil || !bytes.Equal(gotJPEG, jpegRef) {
				t.Fatal("jpeg 参考图不一致")
			}
			payload := `{"data":[{"url":"https://cdn.example/out.png"}],"billing_passed":true}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(payload)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		}
		if r.URL.String() != "https://cdn.example/out.png" {
			t.Fatalf("只应回捞响应里的地址, got %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("跟到另一个主机取 url 时不能带凭证")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(out)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	got, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "浅灰棚拍",
		Refs:       [][]byte{ref, jpegRef},
	})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || !got.HostLive || got.BillingPassed || got.MediaType != "image/png" || !bytes.Equal(got.Bytes, out) {
		t.Fatalf("参考图生成应可解码且未扣费: %+v posts=%d", got, posts)
	}
}

func TestGenerateBadReferenceDoesNotCall(t *testing.T) {
	var calls int
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		t.Fatal("参考图无法解码时不应拨号")
		return nil, nil
	})}
	_, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1/images/generations",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "杯子",
		Refs:       [][]byte{[]byte("not-an-image")},
	})
	if calls != 0 || err == nil || !strings.Contains(err.Error(), "图片无法解码") || strings.Contains(err.Error(), "secret-model-key") {
		t.Fatalf("坏参考图应拒绝且不拨号, calls=%d err=%v", calls, err)
	}
}

func TestGenerateEmptyModelDoesNotCall(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Fatal("空模型名不应拨号")
		return nil, nil
	})}
	_, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1/images/generations",
		Credential: "secret-model-key",
		Prompt:     "杯子",
	})
	if err == nil || err.Error() != "模型名缺失" {
		t.Fatalf("空模型名不能出图, got %v", err)
	}
}

func TestGenerateLoopbackHostsStayUnlive(t *testing.T) {
	pngBytes := solidPNG(t, color.NRGBA{R: 3, G: 3, B: 3, A: 255})
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
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
	for _, endpoint := range []string{
		"http://127.0.0.1:9/v1/images/generations",
		"http://localhost:9/v1/images/generations",
		"http://[::1]:9/v1/images/generations",
	} {
		got, err := Generate(context.Background(), client, Request{
			Endpoint: endpoint, Credential: "secret-model-key", Model: "qwen-image-2.0-pro", Prompt: "杯子",
		})
		if err != nil || got.HostLive || got.BillingPassed || len(got.Bytes) == 0 {
			t.Fatalf("%s 回环不能算供应商出图: err=%v hostLive=%v billing=%v n=%d", endpoint, err, got.HostLive, got.BillingPassed, len(got.Bytes))
		}
	}
}

func TestCanvasSizeStaysInsideModelDomain(t *testing.T) {
	if CanvasSize(800, 800) != "800x800" || CanvasSize(1024, 1024) != "1024x1024" {
		t.Fatal("域内电商尺寸应原样送出")
	}
	if CanvasSize(0, 800) != "" || CanvasSize(100, 100) != "" || CanvasSize(4096, 4096) != "" {
		t.Fatal("缺尺寸或域外尺寸不能伪装成已请求")
	}
	if notes := SizeNotes(0, 0, 2, 2); notes != nil {
		t.Fatalf("没写项目尺寸时不应出说明: %#v", notes)
	}
	if notes := SizeNotes(100, 100, 100, 100); len(notes) != 1 || notes[0] != SizeOutOfDomainNote {
		t.Fatalf("域外尺寸应说明未按该尺寸请求: %#v", notes)
	}
	if notes := SizeNotes(800, 800, 5, 4); len(notes) != 1 || notes[0] != SizeMismatchNote {
		t.Fatalf("像素不一致应说明: %#v", notes)
	}
	if notes := SizeNotes(800, 800, 800, 800); notes != nil {
		t.Fatalf("尺寸一致不是通过标记: %#v", notes)
	}
}

func TestGenerateSendsRequestedSizeAndPixelSize(t *testing.T) {
	pngBytes := solidPNG(t, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	var seenSize string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seenSize, _ = body["size"].(string)
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
		Endpoint:   "https://images.example/v1",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "杯子",
		Size:       "800x800",
	})
	if err != nil || seenSize != "800x800" || got.WidthPx != 2 || got.HeightPx != 2 || got.BillingPassed {
		t.Fatalf("应送出请求尺寸并读回像素: size=%s err=%v %+v", seenSize, err, got)
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

func TestGenerateKeepsImageCountOutOfTheUserPrice(t *testing.T) {
	pngBytes := solidPNG(t, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := json.Marshal(map[string]any{
			"data":  []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)}},
			"usage": map[string]any{"image_count": 1, "price": 9.9},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	got, err := Generate(context.Background(), client, Request{
		Endpoint:   "https://images.example/v1/images/generations",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "杯子",
	})
	if err != nil || got.ImageCount != 1 || got.UsageNote != "供应商返回 1 张，不是用户报价" || got.BillingPassed {
		t.Fatalf("张数不能变成报价: err=%v %+v", err, got)
	}
	if strings.Contains(got.UsageNote, "9.9") {
		t.Fatalf("用量说明不能带上回价: %s", got.UsageNote)
	}
	plain := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := json.Marshal(map[string]any{
			"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(pngBytes)}},
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(raw)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	missing, err := Generate(context.Background(), plain, Request{
		Endpoint:   "https://images.example/v1/images/generations",
		Credential: "secret-model-key",
		Model:      "qwen-image-2.0-pro",
		Prompt:     "杯子",
	})
	if err != nil || missing.ImageCount != 0 || missing.UsageNote != "供应商用量未返回" || missing.BillingPassed {
		t.Fatalf("没有用量时应明确未返回: err=%v %+v", err, missing)
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

func solidJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
