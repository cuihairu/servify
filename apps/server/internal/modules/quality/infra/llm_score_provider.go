package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/quality/application"

	"servify/apps/server/internal/platform/llm"
)

// LLMRuntimeConfig LLM 打分的运行参数（来自 config.QualityConfig.LLM）。
type LLMRuntimeConfig struct {
	Model          string // 空则用 provider 默认模型
	Temperature    float64
	MaxInputChars  int // transcript 总预算
	MaxTurnChars   int // 单轮截断
	TimeoutSeconds int
	MaxTokens      int
}

// LLMScoreProvider 基于 platform/llm 的打分实现：prompt 组装 → 调用 →
// 剥围栏解析 JSON → 维度校验 → 加权总分。任何解析失败包装
// application.ErrInvalidScoreOutput 由服务侧计入重试。
type LLMScoreProvider struct {
	provider llm.LLMProvider
	cfg      LLMRuntimeConfig
}

func NewLLMScoreProvider(provider llm.LLMProvider, cfg LLMRuntimeConfig) *LLMScoreProvider {
	if cfg.MaxInputChars <= 0 {
		cfg.MaxInputChars = 12000
	}
	if cfg.MaxTurnChars <= 0 {
		cfg.MaxTurnChars = 500
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 800
	}
	return &LLMScoreProvider{provider: provider, cfg: cfg}
}

// llmScoreOutput LLM 期望输出的 JSON 结构。
type llmScoreOutput struct {
	Dimensions map[string]struct {
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	} `json:"dimensions"`
	Summary string `json:"summary"`
}

func (p *LLMScoreProvider) ScoreSession(ctx context.Context, req application.ScoreRequest) (application.ScoreResult, error) {
	messages := []llm.ChatMessage{
		{Role: "system", Content: p.systemPrompt(req.Dimensions)},
		{Role: "user", Content: p.transcript(req.Turns)},
	}

	callCtx := ctx
	cancel := func() {}
	if p.cfg.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(p.cfg.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	resp, err := p.provider.Chat(callCtx, llm.ChatRequest{
		Model:       p.cfg.Model,
		Messages:    messages,
		Temperature: p.cfg.Temperature,
		MaxTokens:   p.cfg.MaxTokens,
	})
	if err != nil {
		return application.ScoreResult{}, fmt.Errorf("quality: llm chat: %w", err)
	}

	out, err := parseScoreOutput(resp.Content, req.Dimensions)
	if err != nil {
		return application.ScoreResult{}, err
	}

	dimensions := make(map[string]application.DimensionScore, len(out.Dimensions))
	total, weightSum := 0.0, 0.0
	for _, d := range req.Dimensions {
		ds := out.Dimensions[d.Key]
		dimensions[d.Key] = application.DimensionScore{Score: ds.Score, Weight: d.Weight, Reason: ds.Reason}
		total += ds.Score * d.Weight
		weightSum += d.Weight
	}
	if weightSum > 0 {
		total /= weightSum
	}

	provider := resp.Provider
	if provider == "" {
		provider = "llm"
	}
	model := resp.Model
	if model == "" {
		model = p.cfg.Model
	}
	return application.ScoreResult{
		TotalScore: total,
		Dimensions: dimensions,
		Summary:    out.Summary,
		Provider:   provider,
		Model:      model,
	}, nil
}

func (p *LLMScoreProvider) systemPrompt(dimensions []application.ScoringDimension) string {
	var b strings.Builder
	b.WriteString("你是一名严谨的客服会话质检员。请阅读完整对话记录，对下列每个维度按 0-10 分打分（10 为完美）：\n")
	for _, d := range dimensions {
		fmt.Fprintf(&b, "- %s（%s，权重 %.0f%%）\n", d.Key, d.Prompt, d.Weight*100)
	}
	b.WriteString(`总分由系统按权重加权计算，无需给出。
严格只输出如下 JSON，不要任何其他文字或代码围栏：
{"dimensions":{"<维度key>":{"score":<0-10数字>,"reason":"<简短理由>"}...},"summary":"<一句话总评>"}`)
	return b.String()
}

// transcript 组装对话记录；超出总预算时保留前 2 轮 + 从尾部多装，中间以省略标记衔接。
func (p *LLMScoreProvider) transcript(turns []application.TranscriptTurn) string {
	trimmed := make([]string, 0, len(turns))
	total := 0
	for _, t := range turns {
		line := formatTurn(t)
		if len(line) > p.cfg.MaxTurnChars {
			line = line[:p.cfg.MaxTurnChars]
		}
		trimmed = append(trimmed, line)
		total += len(line) + 1
	}
	if total <= p.cfg.MaxInputChars {
		return strings.Join(trimmed, "\n")
	}

	head := 2
	if head > len(trimmed) {
		head = len(trimmed)
	}
	budget := p.cfg.MaxInputChars
	used := 0
	for _, line := range trimmed[:head] {
		used += len(line) + 1
	}
	if used > budget {
		return strings.Join(trimmed[:head], "\n")
	}
	kept := head
	for kept < len(trimmed) {
		l := len(trimmed[len(trimmed)-1-(kept-head)]) + 1
		if used+l > budget {
			break
		}
		used += l
		kept++
	}
	omitted := len(trimmed) - kept
	var b strings.Builder
	b.WriteString(strings.Join(trimmed[:head], "\n"))
	if omitted > 0 && kept > head {
		fmt.Fprintf(&b, "\n…（中间省略 %d 轮）…\n", omitted)
		b.WriteString(strings.Join(trimmed[len(trimmed)-(kept-head):], "\n"))
	} else if omitted > 0 {
		fmt.Fprintf(&b, "\n…（其余 %d 轮省略）…", omitted)
	}
	return b.String()
}

func formatTurn(t application.TranscriptTurn) string {
	return fmt.Sprintf("[%s] %s: %s", t.At.Format("01-02 15:04"), t.Role, strings.ReplaceAll(t.Content, "\n", " "))
}

// parseScoreOutput 剥掉可能的 markdown 围栏后解析并校验维度。
func parseScoreOutput(content string, dimensions []application.ScoringDimension) (*llmScoreOutput, error) {
	raw := stripJSONFences(content)
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("%w: no json object in response", application.ErrInvalidScoreOutput)
	}
	var out llmScoreOutput
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("%w: %v", application.ErrInvalidScoreOutput, err)
	}
	if len(out.Dimensions) == 0 {
		return nil, fmt.Errorf("%w: missing dimensions", application.ErrInvalidScoreOutput)
	}
	for _, d := range dimensions {
		got, ok := out.Dimensions[d.Key]
		if !ok {
			return nil, fmt.Errorf("%w: missing dimension %q", application.ErrInvalidScoreOutput, d.Key)
		}
		if got.Score < 0 || got.Score > 10 {
			return nil, fmt.Errorf("%w: dimension %q score %v out of range", application.ErrInvalidScoreOutput, d.Key, got.Score)
		}
	}
	return &out, nil
}

func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
