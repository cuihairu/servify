package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/tts"
)

// 编译期契约断言：实现必须满足 tts.Synthesizer。
var _ tts.Synthesizer = (*Provider)(nil)

type capturedRequest struct {
	method string
	path   string
	auth   string
	body   speechRequest
}

// newSpeechServer 起一个可编程的 OpenAI 兼容 TTS 端点：handler 返回状态码
// 与响应体，请求经 captured 通道回传供断言。
func newSpeechServer(t *testing.T, status int, respond []byte) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	captured := make(chan capturedRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := capturedRequest{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization")}
		_ = json.Unmarshal(body, &req.body)
		captured <- req
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(respond)
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestSynthesizeHappyPath(t *testing.T) {
	srv, captured := newSpeechServer(t, http.StatusOK, []byte("FAKE-MP3-BYTES"))
	p := NewProvider(Config{APIKey: "key-1", BaseURL: srv.URL + "/", Model: "tts-1-hd", Voice: "nova", Format: "wav", Timeout: 2 * time.Second})

	resp, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "你好世界", Lang: "zh-CN"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(resp.Audio) != "FAKE-MP3-BYTES" || resp.Format != "wav" {
		t.Fatalf("response degraded: %+v", resp)
	}
	req := <-captured
	if req.method != http.MethodPost || req.path != "/audio/speech" {
		t.Fatalf("request line = %s %s", req.method, req.path)
	}
	if req.auth != "Bearer key-1" {
		t.Fatalf("auth header = %q", req.auth)
	}
	if req.body.Model != "tts-1-hd" || req.body.Input != "你好世界" || req.body.Voice != "nova" || req.body.ResponseFormat != "wav" {
		t.Fatalf("speech body degraded: %+v", req.body)
	}
}

func TestSynthesizeDefaults(t *testing.T) {
	// 注意：默认 base_url 指向真实官方端点，测试必须封闭——只把 base_url
	// 换成本地端点，model/voice/format 留空以验证 provider 侧默认兜底。
	srv, captured := newSpeechServer(t, http.StatusOK, []byte("audio"))
	p := NewProvider(Config{BaseURL: srv.URL})

	resp, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if string(resp.Audio) != "audio" || resp.Format != "mp3" {
		t.Fatalf("default format not applied: %+v", resp)
	}
	req := <-captured
	if req.auth != "" {
		t.Fatalf("empty api key must not send auth header, got %q", req.auth)
	}
	if req.body.Model != defaultModel || req.body.Voice != defaultVoice || req.body.ResponseFormat != defaultFormat {
		t.Fatalf("defaults not applied: %+v", req.body)
	}
}

func TestSynthesizeBlankTextRejected(t *testing.T) {
	p := NewProvider(Config{})
	for _, text := range []string{"", "   "} {
		if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: text}); !errors.Is(err, tts.ErrInvalidRequest) {
			t.Fatalf("blank text %q err = %v, want ErrInvalidRequest", text, err)
		}
	}
}

func TestSynthesizeUpstreamErrorBody(t *testing.T) {
	srv, _ := newSpeechServer(t, http.StatusTooManyRequests, []byte(`{"error":{"message":"quota exceeded"}}`))
	p := NewProvider(Config{BaseURL: srv.URL})

	_, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"})
	if !errors.Is(err, tts.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
	if !strings.Contains(err.Error(), "quota exceeded") || !strings.Contains(err.Error(), "429") {
		t.Fatalf("error must carry status and upstream message: %v", err)
	}
}

func TestSynthesizeUpstreamErrorPlainBodyTruncated(t *testing.T) {
	long := strings.Repeat("x", 2048)
	srv, _ := newSpeechServer(t, http.StatusInternalServerError, []byte(long))
	p := NewProvider(Config{BaseURL: srv.URL})

	_, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"})
	if !errors.Is(err, tts.ErrUpstream) {
		t.Fatalf("err = %v, want ErrUpstream", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 600)) {
		t.Fatal("plain error body must be truncated to 512 bytes")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error must carry status: %v", err)
	}
}

func TestSynthesizeNetworkFailure(t *testing.T) {
	srv, _ := newSpeechServer(t, http.StatusOK, []byte("audio"))
	url := srv.URL
	srv.Close()
	p := NewProvider(Config{BaseURL: url})

	if _, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"}); err == nil {
		t.Fatal("synthesis against closed server must fail")
	}
}

func TestSynthesizeBuildRequestFailure(t *testing.T) {
	// base_url 含非法字符（空格）→ NewRequest 失败，走 build request 分支。
	p := NewProvider(Config{BaseURL: "http://exa mple.com/v1"})
	_, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("err = %v, want build request failure", err)
	}
}

func TestSynthesizeReadAudioFailure(t *testing.T) {
	// 响应体声明 100 字节只发 5 字节：服务端中断 → 客户端读体 unexpected EOF。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	p := NewProvider(Config{BaseURL: srv.URL})

	_, err := p.Synthesize(context.Background(), tts.SynthesizeRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "read audio") {
		t.Fatalf("err = %v, want read audio failure", err)
	}
}

func TestHealthCheckBuildRequestFailure(t *testing.T) {
	p := NewProvider(Config{BaseURL: "http://exa mple.com/v1"})
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("malformed base URL must fail health check")
	}
}

func TestHealthCheckVariants(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		srv, captured := newSpeechServer(t, http.StatusOK, []byte(`{"data":[]}`))
		p := NewProvider(Config{APIKey: "k", BaseURL: srv.URL})
		if err := p.HealthCheck(context.Background()); err != nil {
			t.Fatalf("HealthCheck: %v", err)
		}
		req := <-captured
		if req.method != http.MethodGet || req.path != "/models" || req.auth != "Bearer k" {
			t.Fatalf("health request = %s %s auth %q", req.method, req.path, req.auth)
		}
	})
	t.Run("unhealthy", func(t *testing.T) {
		srv, _ := newSpeechServer(t, http.StatusUnauthorized, []byte(`{"error":{"message":"bad key"}}`))
		p := NewProvider(Config{BaseURL: srv.URL})
		if err := p.HealthCheck(context.Background()); !errors.Is(err, tts.ErrUpstream) {
			t.Fatalf("err = %v, want ErrUpstream", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		srv, _ := newSpeechServer(t, http.StatusOK, []byte("{}"))
		url := srv.URL
		srv.Close()
		p := NewProvider(Config{BaseURL: url})
		if err := p.HealthCheck(context.Background()); err == nil {
			t.Fatal("health against closed server must fail")
		}
	})
}

func TestProviderDefaultsStructure(t *testing.T) {
	p := NewProvider(Config{BaseURL: "https://gateway.example/v1///"})
	if p.baseURL != "https://gateway.example/v1" {
		t.Fatalf("trailing slashes must be trimmed, got %q", p.baseURL)
	}
	p2 := NewProvider(Config{})
	if p2.baseURL != defaultBaseURL || p2.model != defaultModel || p2.voice != defaultVoice ||
		p2.format != defaultFormat || p2.client.Timeout != defaultTimeout {
		t.Fatalf("constructor defaults degraded: %+v", p2)
	}
}
