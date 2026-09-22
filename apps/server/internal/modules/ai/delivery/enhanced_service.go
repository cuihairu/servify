package delivery

import (
	"context"
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
func NewOrchestratedEnhancedAIService(
	base *AIService,
	llmProvider llm.LLMProvider,
	knowledgeProvider knowledgeprovider.KnowledgeProvider,
	knowledgeProviderID string,
	logger *logrus.Logger,
) *OrchestratedEnhancedAIService {
	if logger == nil {
		logger = logrus.New()
	}
	if knowledgeProvider != nil && strings.TrimSpace(knowledgeProviderID) == "" {
		knowledgeProviderID = "weknora"
	}
	orchestrator := aimodule.NewQueryOrchestrator(llmProvider, knowledgeProvider)
	registry := aimodule.NewToolRegistry()
	registry.Register(aimodule.NewCustomerLookupTool(nil))
	registry.Register(aimodule.NewTicketLookupTool(nil))
	registry.Register(aimodule.NewHandoffTool(nil))
	toolExecutor := aimodule.NewToolExecutor(registry, nil)
	orchestrator.SetToolExecutor(toolExecutor)
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
		s.promMetrics.RecordAIRequest("internal", "", "success", "transfer", time.Since(start).Seconds())
		return &EnhancedAIResponse{
			AIResponse: &AIResponse{
				Content:    "我来为您转接人工客服，请稍等...",
				Source:     "system",
				Confidence: 1.0,
			},
			Strategy: "transfer",
			Duration: time.Since(start),
		}, nil
	}

	result, err := s.activeOrchestrator().Handle(ctx, aimodule.AIRequest{
		TaskType:       aimodule.TaskTypeQA,
		ConversationID: sessionID,
		Query:          query,
		SystemPrompt:   "你是 Servify 智能客服助手，请基于上下文给出准确、简洁、专业的中文回答。",
		// 模型参数来自 ai.provider 对应配置族（见 WithRuntimeParams）：
		// 此前 config 里 model/temperature/max_tokens 一直是死配置，这里
		// 是它们唯一生效的入口。
		Model:       s.runtimeParams.Model,
		Temperature: s.runtimeParams.Temperature,
		MaxTokens:   s.runtimeParams.MaxTokens,
		TimeoutMs:   s.runtimeParams.TimeoutMs,
		// 多轮上下文：历史由 SessionHistoryLoader 拉取（零注入 = 单轮），
		// 当前 query 恒为最后一条 user 消息（PromptBuilder 的分叉约定）。
		Messages: s.buildHistoryMessages(ctx, query, sessionID),
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
	})
	if err == nil && result == nil {
		// Handle 契约外仍可能 (nil, nil)（如 llmProvider 未接线）：
		// 归一成错误走兜底分支，避免对 result.Content 解引用 panic。
		err = fmt.Errorf("ai orchestrator returned empty result (session_id=%s)", sessionID)
	}
	if err != nil {
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
	// 置信门：低于阈值只产出"建议转人工"的元数据（next_action/handoff_reason），
	// 不改写答案内容、不执行转接——真正转接仍由用户显式发起或关键词触发。
	if s.runtimeParams.HandoffEnabled && aiResp.Confidence < s.runtimeParams.HandoffConfidenceThreshold {
		aiResp.NextAction = "handoff"
		aiResp.HandoffReason = "low_confidence"
	}
	s.promMetrics.RecordAIRequest(s.aiProviderLabel(), "", "success", recordedStrategy, result.Latency.Seconds())
	if result.TokenUsage != nil {
		enhanced.TokensUsed = result.TokenUsage.TotalTokens
		s.promMetrics.RecordAILLMTokens(s.aiProviderLabel(), "input", result.TokenUsage.InputTokens)
		s.promMetrics.RecordAILLMTokens(s.aiProviderLabel(), "output", result.TokenUsage.OutputTokens)
	}
	return enhanced, nil
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
		}
		return s.orchestrator
	}
	o := aimodule.NewQueryOrchestrator(s.llmProvider, provider)
	if s.toolExecutor != nil {
		o.SetToolExecutor(s.toolExecutor)
	}
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
