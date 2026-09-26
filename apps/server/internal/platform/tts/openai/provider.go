// Package openai tts.Synthesizer 的 OpenAI 兼容实现（Phase 2 刀二，
// docs/realtime-translation-design.md §3.1"TTS：OpenAI 兼容 TTS 口起步"）：
// POST {base_url}/audio/speech，逐句整段合成、音频字节整包返回。
// base_url 可指向任意 OpenAI 兼容网关；非流式口径，chunked 流式留 Phase 3。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/platform/tts"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "tts-1"
	defaultVoice   = "alloy"
	defaultFormat  = "mp3"
	defaultTimeout = 30 * time.Second
)

// Config provider 接入参数（factory 从 config.TTSConfig 平移）。
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Voice   string
	Format  string
	Timeout time.Duration
}

// speechRequest /v1/audio/speech 请求体。
type speechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type errorBody struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Provider OpenAI 兼容 TTS 实现。
type Provider struct {
	apiKey  string
	baseURL string
	model   string
	voice   string
	format  string
	client  *http.Client
}

// NewProvider 构造 provider；零值字段按默认兜底（base_url 默认官方口）。
func NewProvider(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	voice := strings.TrimSpace(cfg.Voice)
	if voice == "" {
		voice = defaultVoice
	}
	format := strings.TrimSpace(cfg.Format)
	if format == "" {
		format = defaultFormat
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Provider{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   model,
		voice:   voice,
		format:  format,
		client: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Synthesize 逐句整段合成：文本非空校验 → POST audio/speech → 音频字节。
// 非 2xx 一律包装 ErrUpstream（状态码与响应体保留在错误链，供管线日志）。
func (p *Provider) Synthesize(ctx context.Context, req tts.SynthesizeRequest) (tts.SynthesizeResponse, error) {
	if strings.TrimSpace(req.Text) == "" {
		return tts.SynthesizeResponse{}, fmt.Errorf("%w: blank text", tts.ErrInvalidRequest)
	}
	// 纯字符串字段结构 json.Marshal 不会失败，忽略错误安全。
	body, _ := json.Marshal(speechRequest{
		Model:          p.model,
		Input:          req.Text,
		Voice:          p.voice,
		ResponseFormat: p.format,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return tts.SynthesizeResponse{}, fmt.Errorf("tts openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return tts.SynthesizeResponse{}, fmt.Errorf("tts openai: do request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return tts.SynthesizeResponse{}, fmt.Errorf("tts openai: read audio: %w", err)
	}
	if resp.StatusCode >= 400 {
		return tts.SynthesizeResponse{}, fmt.Errorf("%w: status %d: %s",
			tts.ErrUpstream, resp.StatusCode, trimErrorMessage(payload))
	}
	return tts.SynthesizeResponse{Audio: payload, Format: p.format}, nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: health status %d: %s", tts.ErrUpstream, resp.StatusCode, trimErrorMessage(body))
	}
	return nil
}

// trimErrorMessage 截取错误响应体的可读部分（error.message 优先，限长防日志刷屏）。
func trimErrorMessage(payload []byte) string {
	var parsed errorBody
	if err := json.Unmarshal(payload, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	msg := string(payload)
	const maxLen = 512
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	return msg
}
