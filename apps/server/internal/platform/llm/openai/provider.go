package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/llm"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type request struct {
	Model       string           `json:"model"`
	Messages    []requestMessage `json:"messages"`
	Tools       []requestTool    `json:"tools,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

type requestMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCalls  []requestToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type requestToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function requestToolCallFunction `json:"function"`
}

type requestToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type requestTool struct {
	Type     string              `json:"type"`
	Function requestToolFunction `json:"function"`
}

type requestToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type response struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Provider is the OpenAI implementation of llm.LLMProvider.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewProvider(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Provider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

func (p *Provider) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	var out llm.ChatResponse
	callCtx, cancel := llm.WithRequestTimeout(ctx, req.Options, 30*time.Second)
	defer cancel()

	err := llm.Retry(callCtx, req.Options.RetryPolicy, func(runCtx context.Context) error {
		resp, err := p.chatOnce(runCtx, req)
		if err != nil {
			return err
		}
		out = resp
		return nil
	})
	return out, err
}

func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	ch := make(chan llm.ChatChunk)
	go func() {
		defer close(ch)
		err := p.streamOnce(ctx, req, ch)
		if err != nil {
			select {
			case <-ctx.Done():
			case ch <- llm.ChatChunk{ContentDelta: fmt.Sprintf("stream error: %v", err), Done: true}:
			}
		}
	}()
	return ch, nil
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, &llm.ProviderError{
		Provider:  "openai",
		Code:      llm.ProviderErrorNotSupported,
		Message:   "openai embed not implemented yet",
		Retryable: false,
	}
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
		return llm.HTTPError("openai", resp.StatusCode, string(body))
	}
	return nil
}

func (p *Provider) chatOnce(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	payload := request{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
		Messages:    make([]requestMessage, 0, len(req.Messages)),
		Tools:       make([]requestTool, 0, len(req.Tools)),
	}
	if payload.Model == "" {
		payload.Model = config.DefaultOpenAIModel
	}
	for _, msg := range req.Messages {
		m := requestMessage{Role: msg.Role, Content: msg.Content}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = make([]requestToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				args := tc.Arguments
				argsJSON, _ := json.Marshal(args)
				m.ToolCalls = append(m.ToolCalls, requestToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: requestToolCallFunction{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		if msg.Role == "tool" {
			m.ToolCallID = msg.ToolCallID
		}
		payload.Messages = append(payload.Messages, m)
	}
	for _, tool := range req.Tools {
		payload.Tools = append(payload.Tools, requestTool{
			Type: "function",
			Function: requestToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("create openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, &llm.ProviderError{
			Provider:  "openai",
			Code:      llm.ProviderErrorUnavailable,
			Message:   "send openai request failed",
			Retryable: true,
			Cause:     err,
		}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return llm.ChatResponse{}, fmt.Errorf("read openai response: %w", err)
	}

	if httpResp.StatusCode >= 400 {
		return llm.ChatResponse{}, llm.HTTPError("openai", httpResp.StatusCode, string(respBody))
	}

	var resp response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return llm.ChatResponse{}, fmt.Errorf("decode openai response: %w", err)
	}
	if resp.Error != nil {
		return llm.ChatResponse{}, &llm.ProviderError{
			Provider:  "openai",
			Code:      llm.ProviderErrorUpstream,
			Message:   resp.Error.Message,
			Retryable: false,
		}
	}
	if len(resp.Choices) == 0 {
		return llm.ChatResponse{}, &llm.ProviderError{
			Provider:  "openai",
			Code:      llm.ProviderErrorUpstream,
			Message:   "empty choices",
			Retryable: false,
		}
	}

	out := llm.ChatResponse{
		Content:      resp.Choices[0].Message.Content,
		Provider:     "openai",
		Model:        payload.Model,
		FinishReason: resp.Choices[0].FinishReason,
		ToolCalls:    decodeToolCalls(resp.Choices[0].Message.ToolCalls),
	}
	if resp.Usage != nil {
		out.TokenUsage = &llm.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
	}
	return out, nil
}

// streamTypes for SSE parsing.
type streamEvent struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Content   string           `json:"content,omitempty"`
	Role      string           `json:"role,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    int            `json:"index"`
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type,omitempty"`
	Function streamFunction `json:"function,omitempty"`
}

type streamFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// streamingToolAccumulator accumulates streaming tool call deltas by index.
type streamingToolAccumulator struct {
	index int
	id    string
	name  string
	args  string
}

func (p *Provider) streamOnce(ctx context.Context, req llm.ChatRequest, ch chan<- llm.ChatChunk) error {
	payload := request{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
		Messages:    make([]requestMessage, 0, len(req.Messages)),
		Tools:       make([]requestTool, 0, len(req.Tools)),
	}
	if payload.Model == "" {
		payload.Model = config.DefaultOpenAIModel
	}
	for _, msg := range req.Messages {
		m := requestMessage{Role: msg.Role, Content: msg.Content}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = make([]requestToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				m.ToolCalls = append(m.ToolCalls, requestToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: requestToolCallFunction{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		if msg.Role == "tool" {
			m.ToolCallID = msg.ToolCallID
		}
		payload.Messages = append(payload.Messages, m)
	}
	for _, tool := range req.Tools {
		payload.Tools = append(payload.Tools, requestTool{
			Type: "function",
			Function: requestToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal openai stream request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create openai stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return &llm.ProviderError{
			Provider:  "openai",
			Code:      llm.ProviderErrorUnavailable,
			Message:   "send openai stream request failed",
			Retryable: true,
			Cause:     err,
		}
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(httpResp.Body)
		return llm.HTTPError("openai", httpResp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	// Accumulate tool calls across chunks.
	toolAccums := make(map[int]*streamingToolAccumulator)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			// Emit any accumulated tool calls before finishing.
			emitToolResults(ch, toolAccums)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- llm.ChatChunk{Done: true}:
			}
			return nil
		}

		var evt streamEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		if len(evt.Choices) == 0 {
			continue
		}
		choice := evt.Choices[0]

		// Emit content delta.
		if choice.Delta.Content != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- llm.ChatChunk{ContentDelta: choice.Delta.Content}:
			}
		}

		// Accumulate tool call deltas.
		for _, tc := range choice.Delta.ToolCalls {
			acc, ok := toolAccums[tc.Index]
			if !ok {
				acc = &streamingToolAccumulator{index: tc.Index}
				toolAccums[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.args += tc.Function.Arguments
			}
		}

		// If finish reason is set and non-null, emit remaining.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			emitToolResults(ch, toolAccums)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ch <- llm.ChatChunk{Done: true}:
			}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read openai stream: %w", err)
	}

	// Stream ended without [DONE] or finish_reason.
	emitToolResults(ch, toolAccums)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- llm.ChatChunk{Done: true}:
	}
	return nil
}

func emitToolResults(ch chan<- llm.ChatChunk, accums map[int]*streamingToolAccumulator) {
	for _, acc := range accums {
		args := map[string]interface{}{}
		if strings.TrimSpace(acc.args) != "" {
			_ = json.Unmarshal([]byte(acc.args), &args)
		}
		select {
		case ch <- llm.ChatChunk{
			ToolCall: &llm.ToolCall{
				ID:        acc.id,
				Name:      acc.name,
				Arguments: args,
			},
		}:
		default:
		}
	}
	// Clear accumulators.
	for k := range accums {
		delete(accums, k)
	}
}

func decodeToolCalls(toolCalls []struct {
	ID       string "json:\"id\""
	Type     string "json:\"type\""
	Function struct {
		Name      string "json:\"name\""
		Arguments string "json:\"arguments\""
	} "json:\"function\""
}) []llm.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		args := map[string]interface{}{}
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
		}
		out = append(out, llm.ToolCall{
			ID:        toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: args,
		})
	}
	return out
}
