package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"servify/apps/server/internal/platform/llm"

	"github.com/sirupsen/logrus"
)

// CopilotAction 标识坐席 AI 辅助动作类型。
type CopilotAction string

const (
	// CopilotSuggestReply 基于会话近期历史为坐席起草下一条客户回复。
	CopilotSuggestReply CopilotAction = "suggest_reply"
	// CopilotRewrite 按指定语气改写坐席草稿（一键改写）。
	CopilotRewrite CopilotAction = "rewrite"
	// CopilotSummary 生成会话三段式摘要（问题/处理过程/结论待办）。
	CopilotSummary CopilotAction = "session_summary"
)

// 哨兵错误：handler 依此映射 400（参数问题）与 503（AI 未配置），
// 其余错误一律按模型/下游故障映射 500。
var (
	ErrCopilotUnsupportedAction = errors.New("unsupported copilot action")
	ErrCopilotSessionRequired   = errors.New("session_id is required for this action")
	ErrCopilotDraftRequired     = errors.New("draft is required for rewrite")
	ErrCopilotEmptyHistory      = errors.New("session has no usable messages")
	ErrCopilotUnavailable       = errors.New("ai copilot is not configured")
)

// CopilotRequest 坐席辅助请求。action=suggest_reply / session_summary
// 必填 session_id；action=rewrite 必填 draft，tone 可选
// （professional/friendly/concise，缺省 professional，其余值透传给提示词）。
type CopilotRequest struct {
	Action    CopilotAction `json:"action"`
	SessionID string        `json:"session_id,omitempty"`
	Draft     string        `json:"draft,omitempty"`
	Tone      string        `json:"tone,omitempty"`
}

// CopilotResponse 单个辅助动作的产出；Text 即可直接展示/采用的结果文本。
type CopilotResponse struct {
	Action    CopilotAction `json:"action"`
	SessionID string        `json:"session_id,omitempty"`
	Text      string        `json:"text"`
}

const (
	copilotSuggestSystemPrompt = "你是客服坐席的 AI 助手。请根据对话历史，为坐席起草下一条回复客户的消息。" +
		"要求：语气专业友善；准确回应客户最新诉求；信息不足时礼貌地请客户补充。" +
		"直接输出回复文本，不要任何解释、前缀或代码块。"
	copilotRewriteSystemPrompt = "你是客服写作助手。请把坐席给出的草稿改写为%s的语气，" +
		"保持事实、承诺与关键信息完全不变，只优化措辞与表达。直接输出改写后的文本，不要解释。"
	copilotSummarySystemPrompt = "你是客服质检助手。请用中文按以下三段式总结会话：\n" +
		"1. 客户问题：\n2. 处理过程：\n3. 结论/待办：\n直接输出总结，不要额外解释。"
)

// AgentCopilotService 为人工坐席提供"点一下就能用"的 AI 辅助：回复建议、
// 一键改写、会话摘要。与客户首答共用 LLM provider 与出站参数（AIRuntimeParams），
// 会话历史口复用 SessionHistoryLoader 窄接口，由装配层事后回填。
// 当前为启动期全局装配，不跟随租户/工作区作用域模型覆盖（与 scoped 首答
// 链路的边界见 attachSessionHistory 注释）。
type AgentCopilotService struct {
	llm           llm.LLMProvider
	historyLoader SessionHistoryLoader
	params        AIRuntimeParams
	logger        *logrus.Logger
}

// NewAgentCopilotService 构造坐席辅助服务；llm 为 nil 时端点降级为 503。
func NewAgentCopilotService(llm llm.LLMProvider, params AIRuntimeParams) *AgentCopilotService {
	return &AgentCopilotService{llm: llm, params: params, logger: logrus.StandardLogger()}
}

// WithSessionHistory 回填会话历史读取口（conversation service 晚于 AI 装配
// 构建，与首答链路同用装配事后回填模式）；nil 安全。
func (s *AgentCopilotService) WithSessionHistory(loader SessionHistoryLoader) *AgentCopilotService {
	if s == nil {
		return s
	}
	s.historyLoader = loader
	return s
}

// Copilot 分发坐席辅助动作。历史类动作（建议回复/摘要）在会话无可读消息时
// 返回 ErrCopilotEmptyHistory——坐席工具宁可明确失败，不凭空编造上下文。
func (s *AgentCopilotService) Copilot(ctx context.Context, req CopilotRequest) (*CopilotResponse, error) {
	if s == nil || s.llm == nil {
		return nil, ErrCopilotUnavailable
	}
	sessionID := strings.TrimSpace(req.SessionID)
	switch req.Action {
	case CopilotSuggestReply:
		return s.suggestReply(ctx, req, sessionID)
	case CopilotRewrite:
		return s.rewrite(ctx, req)
	case CopilotSummary:
		return s.summarize(ctx, sessionID)
	default:
		return nil, fmt.Errorf("%w: %q", ErrCopilotUnsupportedAction, req.Action)
	}
}

func (s *AgentCopilotService) suggestReply(ctx context.Context, req CopilotRequest, sessionID string) (*CopilotResponse, error) {
	if sessionID == "" {
		return nil, ErrCopilotSessionRequired
	}
	turns, err := s.historyTurns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, ErrCopilotEmptyHistory
	}
	messages := append(turns, llm.ChatMessage{
		Role:    "user",
		Content: "请基于以上对话，为坐席起草下一条回复客户的消息。只输出回复文本本身。",
	})
	text, err := s.chat(ctx, messages...)
	if err != nil {
		return nil, err
	}
	return &CopilotResponse{Action: CopilotSuggestReply, SessionID: sessionID, Text: text}, nil
}

func (s *AgentCopilotService) rewrite(ctx context.Context, req CopilotRequest) (*CopilotResponse, error) {
	draft := strings.TrimSpace(req.Draft)
	if draft == "" {
		return nil, ErrCopilotDraftRequired
	}
	text, err := s.chat(ctx,
		llm.ChatMessage{Role: "system", Content: fmt.Sprintf(copilotRewriteSystemPrompt, rewriteTone(req.Tone))},
		llm.ChatMessage{Role: "user", Content: "草稿：\n" + draft},
	)
	if err != nil {
		return nil, err
	}
	return &CopilotResponse{Action: CopilotRewrite, Text: text}, nil
}

func (s *AgentCopilotService) summarize(ctx context.Context, sessionID string) (*CopilotResponse, error) {
	if sessionID == "" {
		return nil, ErrCopilotSessionRequired
	}
	turns, err := s.historyTurns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(turns) == 0 {
		return nil, ErrCopilotEmptyHistory
	}
	text, err := s.chat(ctx,
		llm.ChatMessage{Role: "system", Content: copilotSummarySystemPrompt},
		llm.ChatMessage{Role: "user", Content: renderTranscript(turns) + "\n请总结以上会话。"},
	)
	if err != nil {
		return nil, err
	}
	return &CopilotResponse{Action: CopilotSummary, SessionID: sessionID, Text: text}, nil
}

// historyTurns 读取会话近期历史并转为升序 ChatMessage；loader 缺省视为
// 无历史（nil 安全），加载失败上抛（与首答"静默降级"不同：坐席工具要
// 明确失败，不悄悄在残缺上下文上作答）。
func (s *AgentCopilotService) historyTurns(ctx context.Context, sessionID string) ([]llm.ChatMessage, error) {
	if s.historyLoader == nil {
		return nil, nil
	}
	msgs, err := s.historyLoader.ListRecentMessages(ctx, sessionID, aiHistoryTurns)
	if err != nil {
		return nil, fmt.Errorf("load session history: %w", err)
	}
	return convertHistory(msgs), nil
}

// chat 组装出站请求（模型/温度/上限/超时与首答同源）并取回文本。
func (s *AgentCopilotService) chat(ctx context.Context, messages ...llm.ChatMessage) (string, error) {
	resp, err := s.llm.Chat(ctx, llm.ChatRequest{
		Model:       s.params.Model,
		Messages:    messages,
		Temperature: s.params.Temperature,
		MaxTokens:   s.params.MaxTokens,
		Options:     llm.RequestOptions{TimeoutMs: s.params.TimeoutMs},
	})
	if err != nil {
		return "", fmt.Errorf("ai copilot chat: %w", err)
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", errors.New("ai copilot returned empty content")
	}
	return text, nil
}

// rewriteTone 把语气标识映射为提示词用中文描述；未知值透传（允许调用方
// 自定义语气），空值与 professional 归一到"专业"。
func rewriteTone(tone string) string {
	switch strings.TrimSpace(tone) {
	case "", "professional":
		return "专业"
	case "friendly":
		return "亲切友好"
	case "concise":
		return "简洁"
	default:
		return tone
	}
}

// renderTranscript 把升序对话轮转成"客户：…/坐席：…"的纯文本记录，
// 供摘要提示词引用。
func renderTranscript(turns []llm.ChatMessage) string {
	var b strings.Builder
	for _, m := range turns {
		speaker := "客户"
		if m.Role == "assistant" {
			speaker = "坐席"
		}
		b.WriteString(speaker)
		b.WriteString("：")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
