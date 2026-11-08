package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	aimodule "servify/apps/server/internal/modules/ai/application"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/knowledgeprovider"
	"servify/apps/server/internal/platform/llm"
	baseweknora "servify/apps/server/pkg/weknora"

	"github.com/sirupsen/logrus"
)

// AIRuntimeParams 编排出站 LLM 调用的模型参数，由装配层从 ai.provider
// 对应的配置族导出（见 llm factory.RuntimeParams）。零值字段原样透传、
// 由 provider 侧默认兜底——与 AIRequest.Model/Temperature 的既有语义一致。
// Handoff* 是首答置信门参数（零值 = 关闭，与历史行为一致）。
type AIRuntimeParams struct {
	Model       string
	Temperature float64
	MaxTokens   int
	TimeoutMs   int
	// HandoffEnabled 开启"置信不足建议转人工"：首答 confidence 低于
	// HandoffConfidenceThreshold 时置 NextAction=handoff（只给建议字段，
	// 不改写答案内容、不执行转接）。
	HandoffEnabled bool
	// HandoffConfidenceThreshold 置信阈值，(0,1]；默认配置 0.65 恰好落在
	// "零命中 confidence=0.6" 与 "有命中 confidence≥0.7" 之间——开箱即
	// "知识库答不上来才建议转人工"。
	HandoffConfidenceThreshold float64
	// NoEvidence 无知识依据策略：零命中/低置信时按配置澄清、拒答、请求人工
	// 或直答。零值 = 历史行为（零命中直答、低置信直答并按 handoff 门附建议）。
	NoEvidence NoEvidencePolicy
}

// NoEvidenceAction 无依据回答的访客面动作。
type NoEvidenceAction string

const (
	// NoEvidenceDirect 直接作答（历史默认）。
	NoEvidenceDirect NoEvidenceAction = "direct"
	// NoEvidenceClarify 澄清追问，不编造答案。
	NoEvidenceClarify NoEvidenceAction = "clarify"
	// NoEvidenceRefuse 无依据拒答。
	NoEvidenceRefuse NoEvidenceAction = "refuse"
	// NoEvidenceHandoff 请求人工接管。
	NoEvidenceHandoff NoEvidenceAction = "handoff"
)

// NoEvidencePolicy 配置化的无依据回答策略。
type NoEvidencePolicy struct {
	// ZeroHitAction 检索零命中时动作（空值 = direct）。
	ZeroHitAction NoEvidenceAction
	// LowConfidenceAction 检索有命中但置信低于阈值时动作（空值 = direct）。
	LowConfidenceAction NoEvidenceAction
	// ConfidenceThreshold 低置信判定阈值；<=0 关闭低置信门（历史行为）。
	ConfidenceThreshold float64
}

// SessionHistoryLoader 是多轮上下文的最小依赖口：编排服务只关心"按会话
// 取最近 N 条消息"，不关心消息如何持久化。conversation 模块的适配器
// 已按此形状实现（models.Message 归一），AI 模块不反向依赖 conversation。
type SessionHistoryLoader interface {
	ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]models.Message, error)
}

// OrchestratedEnhancedAIService keeps the legacy enhanced AI surface while delegating query flow to the AI module.
type OrchestratedEnhancedAIService struct {
	base                     *AIService
	orchestrator             *aimodule.QueryOrchestrator
	llmProvider              llm.LLMProvider
	knowledgeProvider        knowledgeprovider.KnowledgeProvider
	knowledgeProviderID      string
	knowledgeProviderEnabled bool
	fallbackEnabled          bool
	circuitBreaker           *CircuitBreaker
	metrics                  *AIMetrics
	promMetrics              *svcmetrics.BusinessMetrics
	logger                   *logrus.Logger
	toolExecutor             *aimodule.ToolExecutor
	handoffPort              aimodule.HandoffPort
	runtimeParams            AIRuntimeParams
	historyLoader            SessionHistoryLoader
}

// AttachBusinessMetrics 注入进程级 Prometheus 业务指标（nil 安全，可链式）。
// 记录维度：ai_requests_total{provider,outcome,strategy}（primary/fallback/transfer）、
// ai_request_duration_seconds、ai_llm_tokens_total。
func (s *OrchestratedEnhancedAIService) AttachBusinessMetrics(m *svcmetrics.BusinessMetrics) *OrchestratedEnhancedAIService {
	if s == nil {
		return s
	}
	s.promMetrics = m
	return s
}

// WithRuntimeParams 注入出站模型参数（nil 安全，可链式，与
// AttachBusinessMetrics 同款注入风格）。零值 = 不注入 = 历史行为
// （provider 侧默认兜底），忘记链不会改变现状。
func (s *OrchestratedEnhancedAIService) WithRuntimeParams(params AIRuntimeParams) *OrchestratedEnhancedAIService {
	if s == nil {
		return s
	}
	s.runtimeParams = params
	if s.orchestrator != nil {
		s.orchestrator.SetNoEvidencePolicy(orchestratorNoEvidencePolicy{policy: params.NoEvidence})
	}
	return s
}

// WithSessionHistory 注入会话历史读取口（多轮上下文，nil 安全，可链式，
// 与 WithRuntimeParams 同款注入风格）。零值 = 不注入 = 单轮问答的历史行为。
func (s *OrchestratedEnhancedAIService) WithSessionHistory(loader SessionHistoryLoader) *OrchestratedEnhancedAIService {
	if s == nil {
		return s
	}
	s.historyLoader = loader
	return s
}

// aiHistoryTurns 多轮上下文携带的最大历史消息数（不含当前 query）。够覆盖
// "上下文指代 + 追问"场景，同时把 prompt 膨胀与 token 成本控制在个位数
// 消息量级；当前为代码级常量，后续如需按租户调优再配置化。
const aiHistoryTurns = 8

// buildHistoryMessages 构造多轮上下文消息：会话近期消息转 ChatMessage
// （customer→user、agent→assistant，system 与空内容跳过），末尾追加当前
// query（PromptBuilder 只在 Messages 为空时才单独发 query）。ListRecent
// 按 created_at DESC 返回，转成时间升序；最新一条与 query 相同视为已落库
// 的当前消息，去重避免重复提问。任何失败都降级为单轮（返回 nil），不阻塞首答。
func (s *OrchestratedEnhancedAIService) buildHistoryMessages(ctx context.Context, query, sessionID string) []llm.ChatMessage {
	if s == nil || s.historyLoader == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	msgs, err := s.historyLoader.ListRecentMessages(ctx, sessionID, aiHistoryTurns+1)
	if err != nil {
		s.logger.Warnf("AI session history load failed, falling back to single-turn (session_id=%s, error=%v)", sessionID, err)
		return nil
	}
	trimmed := strings.TrimSpace(query)
	chat := convertHistory(msgs)
	// DESC 序里 msgs[0] 是最新一条（转换后位于升序末位）：与当前 query
	// 重复则为已落库的当前消息，去重避免重复提问。
	if n := len(chat); n > 0 && chat[n-1].Content == trimmed {
		chat = chat[:n-1]
	}
	if len(chat) > aiHistoryTurns {
		chat = chat[len(chat)-aiHistoryTurns:]
	}
	return append(chat, llm.ChatMessage{Role: "user", Content: query})
}

// convertHistory 把 DESC 序的会话消息转为时间升序的 ChatMessage：
// customer→user、agent→assistant，system 与空内容跳过。首答多轮上下文
// 与坐席 Copilot 共用同一份转换口径。
func convertHistory(msgs []models.Message) []llm.ChatMessage {
	chat := make([]llm.ChatMessage, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		content := strings.TrimSpace(msgs[i].Content)
		if content == "" {
			continue
		}
		var role string
		switch strings.TrimSpace(msgs[i].Sender) {
		case "customer":
			role = "user"
		case "agent":
			role = "assistant"
		default: // system / 未知发送者不进提示词
			continue
		}
		chat = append(chat, llm.ChatMessage{Role: role, Content: content})
	}
	return chat
}

// aiProviderLabel 返回打点用的 provider 标签；未启用外部 provider 时记 "none"。
func (s *OrchestratedEnhancedAIService) aiProviderLabel() string {
	if id := s.activeKnowledgeProviderID(); id != "" {
		return id
	}
	return "none"
}

// NewOrchestratedEnhancedAIService 组装编排服务。原 weKnoraClient /
// knowledgeBaseID 两参为 write-only 死存储（SyncKnowledgeBase 与上传走
// knowledgeProvider.UpsertDocument），已删——weknora 客户端与 kb id 由
// knowledgeprovider/weknora 的 driver 内部持有。
// AgentToolPorts 是编排服务向 agent 循环广告的三个工具端口。port 为 nil 时对应
// 工具不注册——不把只会报 "port is not configured" 的死工具广告给 LLM（ToolPolicy
// enabled 时 Definitions() 全量注入 agent 循环，模型一调用即烧 step 配额）。
// 已定口径 2026-10-11：三工具保持死路，当前无生产实现可传；接线时在装配处传入
// 非 nil port 即恢复注册与广告。
type AgentToolPorts struct {
	CustomerLookup aimodule.CustomerLookupPort
	TicketLookup   aimodule.TicketLookupPort
	Handoff        aimodule.HandoffPort
}

func NewOrchestratedEnhancedAIService(
	base *AIService,
	llmProvider llm.LLMProvider,
	knowledgeProvider knowledgeprovider.KnowledgeProvider,
	knowledgeProviderID string,
	logger *logrus.Logger,
	toolPorts ...AgentToolPorts,
) *OrchestratedEnhancedAIService {
	if logger == nil {
		logger = logrus.New()
	}
	if knowledgeProvider != nil && strings.TrimSpace(knowledgeProviderID) == "" {
		knowledgeProviderID = "weknora"
	}
	orchestrator := aimodule.NewQueryOrchestrator(llmProvider, knowledgeProvider)
	registry := aimodule.NewToolRegistry()
	for _, ports := range toolPorts {
		if ports.CustomerLookup != nil {
			registry.Register(aimodule.NewCustomerLookupTool(ports.CustomerLookup))
		}
		if ports.TicketLookup != nil {
			registry.Register(aimodule.NewTicketLookupTool(ports.TicketLookup))
		}
		if ports.Handoff != nil {
			registry.Register(aimodule.NewHandoffTool(ports.Handoff))
		}
	}
	toolExecutor := aimodule.NewToolExecutor(registry, nil)
	orchestrator.SetToolExecutor(toolExecutor)
	var handoffPort aimodule.HandoffPort
	for _, ports := range toolPorts {
		if ports.Handoff != nil {
			handoffPort = ports.Handoff
			break
		}
	}
	return &OrchestratedEnhancedAIService{
		base:                     base,
		orchestrator:             orchestrator,
		llmProvider:              llmProvider,
		knowledgeProvider:        knowledgeProvider,
		knowledgeProviderID:      strings.TrimSpace(knowledgeProviderID),
		knowledgeProviderEnabled: knowledgeProvider != nil,
		fallbackEnabled:          true,
		circuitBreaker:           NewCircuitBreaker(),
		metrics:                  &AIMetrics{ActiveKnowledgeProvider: strings.TrimSpace(knowledgeProviderID)},
		logger:                   logger,
		toolExecutor:             toolExecutor,
		handoffPort:              handoffPort,
	}
}

func (s *OrchestratedEnhancedAIService) ProcessQuery(ctx context.Context, query string, sessionID string) (*AIResponse, error) {
	resp, err := s.ProcessQueryEnhanced(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	// 把 enhanced 附加输出（引用来源、产生方式）带回 legacy AIResponse，
	// WS ai-response 帧与 REST 普通查询路径据此透出，无需改接口签名。
	resp.AIResponse.Sources = resp.Sources
	resp.AIResponse.Strategy = resp.Strategy
	return resp.AIResponse, nil
}

func (s *OrchestratedEnhancedAIService) ProcessQueryEnhanced(ctx context.Context, query string, sessionID string) (*EnhancedAIResponse, error) {
	start := time.Now()
	s.metrics.QueryCount++
	if s.ShouldTransferToHuman(query, nil) {
		return s.transferShortCircuit(start), nil
	}

	result, err := s.activeOrchestrator().Handle(ctx, s.buildAIRequest(ctx, query, sessionID))
	if err == nil && result == nil {
		// Handle 契约外仍可能 (nil, nil)（如 llmProvider 未接线）：
		// 归一成错误走兜底分支，避免对 result.Content 解引用 panic。
		err = fmt.Errorf("ai orchestrator returned empty result (session_id=%s)", sessionID)
	}
	if err != nil {
		// 知识服务故障（检索失败/超时）与零命中分开处理：有配置的非直答
		// 策略时按无依据策略响应（reason=knowledge_unavailable），避免把
		// 故障降级成"模型继续编造"；否则维持历史 fallback 兜底。
		var knowledgeErr *aimodule.KnowledgeUnavailableError
		if errors.As(err, &knowledgeErr) {
			action := normalizeNoEvidenceAction(s.runtimeParams.NoEvidence.ZeroHitAction)
			if action != NoEvidenceDirect {
				if s.knowledgeProviderEnabled {
					s.circuitBreaker.OnFailure()
				}
				enhanced := &EnhancedAIResponse{
					AIResponse: &AIResponse{
						Confidence: 0.6,
						Source:     "system",
					},
					Strategy: string(action),
					Duration: time.Since(start),
				}
				s.applyNoEvidenceAction(ctx, sessionID, enhanced.AIResponse, action, "knowledge_unavailable")
				s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", string(action), time.Since(start).Seconds())
				return enhanced, nil
			}
		}
		if s.knowledgeProviderEnabled {
			s.circuitBreaker.OnFailure()
		}
		if s.fallbackEnabled {
			fallback, fbErr := s.base.ProcessQuery(ctx, query, sessionID)
			if fbErr != nil {
				s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "failure", "fallback", time.Since(start).Seconds())
				return nil, fbErr
			}
			s.logger.Warnf("AI enhanced query failed, served by fallback (strategy=fallback, session_id=%s, error=%v)", sessionID, err)
			s.metrics.FallbackUsageCount++
			s.metrics.AverageLatency = time.Since(start)
			s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", "fallback", time.Since(start).Seconds())
			return &EnhancedAIResponse{
				AIResponse: fallback,
				Strategy:   "fallback",
				Duration:   time.Since(start),
			}, nil
		}
		s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "failure", "primary", time.Since(start).Seconds())
		return nil, err
	}
	return s.finalizeEnhanced(ctx, sessionID, result), nil
}

// buildAIRequest 组装首答编排请求：模型参数来自 ai.provider 对应配置族
// （见 WithRuntimeParams）——此前 config 里 model/temperature/max_tokens
// 一直是死配置，这里是它们唯一生效的入口。多轮上下文：历史由
// SessionHistoryLoader 拉取（零注入 = 单轮），当前 query 恒为最后一条
// user 消息（PromptBuilder 的分叉约定）。非流式与流式首答共用。
func (s *OrchestratedEnhancedAIService) buildAIRequest(ctx context.Context, query, sessionID string) aimodule.AIRequest {
	return aimodule.AIRequest{
		TaskType:       aimodule.TaskTypeQA,
		ConversationID: sessionID,
		Query:          query,
		SystemPrompt:   "你是 Servify 智能客服助手，请基于上下文给出准确、简洁、专业的中文回答。",
		Model:          s.runtimeParams.Model,
		Temperature:    s.runtimeParams.Temperature,
		MaxTokens:      s.runtimeParams.MaxTokens,
		TimeoutMs:      s.runtimeParams.TimeoutMs,
		Messages:       s.buildHistoryMessages(ctx, query, sessionID),
		RetrievalPolicy: aimodule.RetrievalPolicy{
			Enabled:   true,
			TopK:      5,
			Threshold: 0.7,
			Strategy:  "semantic",
		},
		ToolPolicy: aimodule.ToolPolicy{
			Enabled:  true,
			MaxSteps: 5,
		},
	}
}

// transferShortCircuit 转人工关键词命中时的统一响应（记录 transfer 指标）。
// 非流式与流式首答共用。
func (s *OrchestratedEnhancedAIService) transferShortCircuit(start time.Time) *EnhancedAIResponse {
	s.promMetrics.RecordAIRequest("internal", "", "success", "transfer", time.Since(start).Seconds())
	return &EnhancedAIResponse{
		AIResponse: &AIResponse{
			Content:    "我来为您转接人工客服，请稍等...",
			Source:     "system",
			Confidence: 1.0,
		},
		Strategy: "transfer",
		Duration: time.Since(start),
	}
}

// finalizeEnhanced 把编排成功结果映射为 enhanced 响应并记录指标：引用来源
// →置信度/产生方式，置信门→转人工建议。非流式与流式首答共用。
func (s *OrchestratedEnhancedAIService) finalizeEnhanced(ctx context.Context, sessionID string, result *aimodule.AIResponse) *EnhancedAIResponse {
	if result != nil && result.NoEvidenceShort {
		action := NoEvidenceAction(result.NoEvidenceAction)
		enhanced := &EnhancedAIResponse{
			AIResponse: &AIResponse{
				Confidence: 0.6,
				Source:     "system",
			},
			Strategy: string(action),
			Duration: result.Latency,
		}
		s.applyNoEvidenceAction(ctx, sessionID, enhanced.AIResponse, action, result.NoEvidenceReason)
		s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", string(action), result.Latency.Seconds())
		return enhanced
	}
	if s.knowledgeProviderEnabled {
		s.circuitBreaker.OnSuccess()
	}
	s.metrics.SuccessCount++
	s.metrics.AverageLatency = result.Latency
	s.metrics.OpenAILatency = result.Latency

	recordedStrategy := "primary"
	aiResp := &AIResponse{
		Content:    result.Content,
		Confidence: confidenceFromSources(result.Sources),
		Source:     "ai",
	}
	enhanced := &EnhancedAIResponse{
		AIResponse: aiResp,
		// 零命中但 LLM 正常作答是"llm"（模型直接回答）而非 fallback——
		// fallback 专指编排失败走 legacy 兜底。历史上这里误标为 fallback，
		// 把能力内的直接回答记成了降级。
		Strategy: "llm",
		Duration: result.Latency,
	}
	zeroHit := len(result.Sources) == 0
	if len(result.Sources) > 0 {
		enhanced.Strategy = s.activeKnowledgeProviderID()
		enhanced.Sources = toWeKnoraSources(result.Sources)
		s.metrics.KnowledgeProviderUsageCount++
		s.metrics.KnowledgeProviderLatency = result.Latency
		switch s.activeKnowledgeProviderID() {
		case "dify":
			s.metrics.DifyUsageCount++
		case "weknora":
			s.metrics.WeKnoraUsageCount++
			s.metrics.WeKnoraLatency = result.Latency
		}
	} else {
		// 零命中直答：主链路成功（strategy=primary），不再计入
		// FallbackUsageCount——那是编排失败兜底专用的指标。
		recordedStrategy = "primary"
	}
	// 无依据策略：零命中或低置信时按配置澄清/拒答/请求人工，避免"不确定仍
	// 编造答案"。direct 保持历史直答（并按下方置信门附建议）。
	policy := s.runtimeParams.NoEvidence
	action := s.noEvidenceAction(zeroHit, aiResp.Confidence, policy)
	if action != "" && action != NoEvidenceDirect {
		reason := "low_confidence"
		if zeroHit {
			reason = "zero_hit"
		}
		s.applyNoEvidenceAction(ctx, sessionID, aiResp, action, reason)
		recordedStrategy = string(action)
		enhanced.Strategy = string(action)
	}
	// 置信门：低于阈值只产出"建议转人工"的元数据（next_action/handoff_reason），
	// 不改写答案内容、不执行转接——真正转接仍由用户显式发起或关键词触发。
	if action == "" || action == NoEvidenceDirect {
		if s.runtimeParams.HandoffEnabled && aiResp.Confidence < s.runtimeParams.HandoffConfidenceThreshold {
			aiResp.NextAction = "handoff"
			aiResp.HandoffReason = "low_confidence"
		}
	}
	s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", recordedStrategy, result.Latency.Seconds())
	if result.TokenUsage != nil {
		enhanced.TokensUsed = result.TokenUsage.TotalTokens
		s.promMetrics.RecordAILLMTokens(s.aiProviderLabel(), "input", result.TokenUsage.InputTokens)
		s.promMetrics.RecordAILLMTokens(s.aiProviderLabel(), "output", result.TokenUsage.OutputTokens)
	}
	return enhanced
}

// noEvidenceAction 计算无依据策略动作：零命中用 ZeroHitAction；有命中但置信
// 低于阈值（门开启）用 LowConfidenceAction；否则 direct。未配置动作归一为
// direct（历史行为）。
func (s *OrchestratedEnhancedAIService) noEvidenceAction(zeroHit bool, confidence float64, policy NoEvidencePolicy) NoEvidenceAction {
	if zeroHit {
		return normalizeNoEvidenceAction(policy.ZeroHitAction)
	}
	if policy.ConfidenceThreshold > 0 && confidence < policy.ConfidenceThreshold {
		return normalizeNoEvidenceAction(policy.LowConfidenceAction)
	}
	return NoEvidenceDirect
}

// applyNoEvidenceAction 改写访客面响应：固定文案（不编造），透出
// next_action（仅 clarify/handoff 有动作语义）与可审计 reason
// （zero_hit/low_confidence/knowledge_unavailable）。
func (s *OrchestratedEnhancedAIService) applyNoEvidenceAction(ctx context.Context, sessionID string, aiResp *AIResponse, action NoEvidenceAction, reason string) {
	aiResp.Reason = reason
	switch action {
	case NoEvidenceClarify:
		aiResp.Content = "抱歉，我暂时无法从知识库中找到与您问题直接相关的信息。请补充更多细节或换种方式描述，我会继续为您查找。"
		aiResp.NextAction = "clarify"
	case NoEvidenceRefuse:
		aiResp.Content = "抱歉，当前没有足够依据回答您的问题。为避免误导，我不会猜测答案，请通过人工客服获取准确帮助。"
	case NoEvidenceHandoff:
		aiResp.Content = "我来为您转接人工客服，请稍等..."
		aiResp.NextAction = "handoff"
		aiResp.HandoffReason = reason
		if s.handoffPort != nil {
			if _, err := s.handoffPort.RequestHandoff(ctx, sessionID, reason); err != nil {
				s.logger.Warnf("No-evidence handoff failed (session_id=%s, reason=%s): %v", sessionID, reason, err)
			}
		}
	}
}

// normalizeNoEvidenceAction 空值/未知值归一为 direct（历史行为）。
func normalizeNoEvidenceAction(action NoEvidenceAction) NoEvidenceAction {
	switch NoEvidenceAction(strings.ToLower(strings.TrimSpace(string(action)))) {
	case NoEvidenceClarify, NoEvidenceRefuse, NoEvidenceHandoff:
		return NoEvidenceAction(strings.ToLower(strings.TrimSpace(string(action))))
	default:
		return NoEvidenceDirect
	}
}

// orchestratorNoEvidencePolicy adapts the configured NoEvidencePolicy to the
// application-layer pre-model short circuit. It evaluates retrieval hits
// before any model/tool call so clarify/refuse/handoff never let the model
// guess or run side-effect tools first.
type orchestratorNoEvidencePolicy struct {
	policy NoEvidencePolicy
}

func (p orchestratorNoEvidencePolicy) EvaluateNoEvidence(ctx context.Context, req aimodule.AIRequest, hits []knowledgeprovider.KnowledgeHit) aimodule.NoEvidenceDecision {
	zeroHit := len(hits) == 0
	action := normalizeNoEvidenceAction(p.policy.ZeroHitAction)
	reason := "zero_hit"
	if !zeroHit {
		conf := confidenceFromSources(hits)
		if p.policy.ConfidenceThreshold <= 0 || conf >= p.policy.ConfidenceThreshold {
			return aimodule.NoEvidenceDecision{}
		}
		action = normalizeNoEvidenceAction(p.policy.LowConfidenceAction)
		reason = "low_confidence"
	}
	if action == NoEvidenceDirect {
		return aimodule.NoEvidenceDecision{}
	}
	return aimodule.NoEvidenceDecision{
		Apply:  true,
		Action: string(action),
		Reason: reason,
	}
}

func (s *OrchestratedEnhancedAIService) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	return aimodule.ShouldTransferToHuman(query, sessionHistory)
}

func (s *OrchestratedEnhancedAIService) GetSessionSummary(messages []models.Message) (string, error) {
	return s.base.GetSessionSummary(messages)
}

func (s *OrchestratedEnhancedAIService) InitializeKnowledgeBase() {
	s.base.InitializeKnowledgeBase()
}

func (s *OrchestratedEnhancedAIService) GetStatus(ctx context.Context) map[string]interface{} {
	documentCount := 0
	if s.base != nil && s.base.knowledgeBase != nil {
		documentCount = len(s.base.knowledgeBase.documents)
	}

	status := map[string]interface{}{
		"type":                       "orchestrated_enhanced",
		"knowledge_provider":         s.activeKnowledgeProviderID(),
		"knowledge_provider_enabled": s.knowledgeProviderEnabled,
		"fallback_enabled":           s.fallbackEnabled,
		"llm_provider":               s.llmProvider != nil,
		"knowledge_mode":             "orchestrated",
		"document_count":             documentCount,
		"metrics":                    s.GetMetrics(),
		"circuit_breaker": map[string]interface{}{
			"state":         s.circuitBreaker.State(),
			"failure_count": s.circuitBreaker.FailureCount(),
		},
	}
	if s.knowledgeProvider != nil {
		err := s.knowledgeProvider.HealthCheck(ctx)
		status["knowledge_provider_healthy"] = err == nil
		if err != nil {
			status["knowledge_provider_error"] = err.Error()
		}
	}
	return status
}

func (s *OrchestratedEnhancedAIService) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	if !s.knowledgeProviderEnabled || s.knowledgeProvider == nil {
		return fmt.Errorf("knowledge provider is not enabled")
	}
	_, err := s.knowledgeProvider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:       title,
		Title:    title,
		Content:  content,
		Tags:     tags,
		Metadata: map[string]interface{}{"source": "manual_upload"},
	})
	return err
}

func (s *OrchestratedEnhancedAIService) GetMetrics() *AIMetrics {
	metrics := *s.metrics
	return &metrics
}

func (s *OrchestratedEnhancedAIService) SetKnowledgeProviderEnabled(enabled bool) {
	s.knowledgeProviderEnabled = enabled
}

func (s *OrchestratedEnhancedAIService) SetFallbackEnabled(enabled bool) {
	s.fallbackEnabled = enabled
}

func (s *OrchestratedEnhancedAIService) ResetCircuitBreaker() {
	s.circuitBreaker.Reset()
}

func (s *OrchestratedEnhancedAIService) SyncKnowledgeBase(ctx context.Context) error {
	if !s.knowledgeProviderEnabled || s.knowledgeProvider == nil {
		return fmt.Errorf("knowledge provider is not enabled")
	}
	for _, doc := range s.base.knowledgeBase.documents {
		if _, err := s.knowledgeProvider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
			ID:       strings.TrimSpace(doc.Title),
			Title:    doc.Title,
			Content:  doc.Content,
			Tags:     strings.Split(doc.Tags, ","),
			Metadata: map[string]interface{}{"category": doc.Category},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *OrchestratedEnhancedAIService) activeKnowledgeProvider() knowledgeprovider.KnowledgeProvider {
	if s.knowledgeProviderEnabled && s.circuitBreaker.Allow() {
		return s.knowledgeProvider
	}
	return nil
}

func (s *OrchestratedEnhancedAIService) activeKnowledgeProviderID() string {
	if !s.knowledgeProviderEnabled {
		return ""
	}
	if strings.TrimSpace(s.knowledgeProviderID) == "" {
		return "weknora"
	}
	return s.knowledgeProviderID
}

func (s *OrchestratedEnhancedAIService) activeOrchestrator() *aimodule.QueryOrchestrator {
	provider := s.activeKnowledgeProvider()
	if provider == s.knowledgeProvider {
		if s.orchestrator == nil {
			s.orchestrator = aimodule.NewQueryOrchestrator(s.llmProvider, provider)
			if s.toolExecutor != nil {
				s.orchestrator.SetToolExecutor(s.toolExecutor)
			}
			s.orchestrator.SetNoEvidencePolicy(orchestratorNoEvidencePolicy{policy: s.runtimeParams.NoEvidence})
		}
		return s.orchestrator
	}
	o := aimodule.NewQueryOrchestrator(s.llmProvider, provider)
	if s.toolExecutor != nil {
		o.SetToolExecutor(s.toolExecutor)
	}
	o.SetNoEvidencePolicy(orchestratorNoEvidencePolicy{policy: s.runtimeParams.NoEvidence})
	return o
}

func toWeKnoraSources(hits []knowledgeprovider.KnowledgeHit) []baseweknora.SearchResult {
	sources := make([]baseweknora.SearchResult, 0, len(hits))
	for _, hit := range hits {
		sources = append(sources, baseweknora.SearchResult{
			DocumentID: hit.DocumentID,
			Title:      hit.Title,
			Content:    hit.Content,
			Score:      hit.Score,
			Source:     hit.Source,
			Metadata:   hit.Metadata,
		})
	}
	return sources
}

func confidenceFromSources(hits []knowledgeprovider.KnowledgeHit) float64 {
	if len(hits) == 0 {
		return 0.6
	}
	score := hits[0].Score
	if score <= 0 {
		return 0.85
	}
	if score > 0.95 {
		return 0.95
	}
	return score
}
