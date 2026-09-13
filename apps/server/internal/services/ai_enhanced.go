package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"servify/apps/server/internal/models"
	"servify/apps/server/pkg/weknora"
)

// EnhancedAIService 外部知识库 provider 集成的增强 AI 服务。
// 当前 legacy 实现仍通过 WeKnora client 工作，但对外补齐通用 provider 语义。
type EnhancedAIService struct {
	// 继承原有 AIService
	*AIService

	// 当前 knowledge provider 兼容实现
	weKnoraClient   weknora.WeKnoraInterface
	weKnoraEnabled  bool
	knowledgeBaseID string

	// 降级和监控
	fallbackEnabled bool
	circuitBreaker  *CircuitBreaker
	metrics         *AIMetrics

	logger *logrus.Logger
}

// AIMetrics AI 服务指标
type AIMetrics struct {
	QueryCount                  int64         `json:"query_count"`
	SuccessCount                int64         `json:"success_count"`
	KnowledgeProviderUsageCount int64         `json:"knowledge_provider_usage_count"`
	DifyUsageCount              int64         `json:"dify_usage_count"`
	WeKnoraUsageCount           int64         `json:"weknora_usage_count"`
	FallbackUsageCount          int64         `json:"fallback_usage_count"`
	AverageLatency              time.Duration `json:"average_latency"`
	KnowledgeProviderLatency    time.Duration `json:"knowledge_provider_latency"`
	WeKnoraLatency              time.Duration `json:"weknora_latency"`
	OpenAILatency               time.Duration `json:"openai_latency"`
	ActiveKnowledgeProvider     string        `json:"active_knowledge_provider,omitempty"`
}

// EnhancedAIResponse 增强的 AI 响应
type EnhancedAIResponse struct {
	*AIResponse
	Sources    []weknora.SearchResult `json:"sources,omitempty"`
	Strategy   string                 `json:"strategy"` // "weknora", "fallback", "hybrid"
	Duration   time.Duration          `json:"duration"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
}

// NewEnhancedAIService 创建增强的 AI 服务
func NewEnhancedAIService(
	originalService *AIService,
	weKnoraClient weknora.WeKnoraInterface,
	knowledgeBaseID string,
	logger *logrus.Logger,
) *EnhancedAIService {
	if logger == nil {
		logger = logrus.New()
	}

	return &EnhancedAIService{
		AIService:       originalService,
		weKnoraClient:   weKnoraClient,
		weKnoraEnabled:  weKnoraClient != nil,
		knowledgeBaseID: knowledgeBaseID,
		fallbackEnabled: true,
		circuitBreaker:  NewCircuitBreaker(),
		metrics:         &AIMetrics{},
		logger:          logger,
	}
}

// ProcessQueryEnhanced 增强的查询处理
func (s *EnhancedAIService) ProcessQueryEnhanced(ctx context.Context, query string, sessionID string) (*EnhancedAIResponse, error) {
	startTime := time.Now()
	s.metrics.QueryCount++

	// 检查是否需要转人工
	if s.ShouldTransferToHuman(query, nil) {
		return &EnhancedAIResponse{
			AIResponse: &AIResponse{
				Content:    "我来为您转接人工客服，请稍等...",
				Source:     "system",
				Confidence: 1.0,
			},
			Strategy: "transfer",
			Duration: time.Since(startTime),
		}, nil
	}

	// 知识检索
	docs, strategy, err := s.retrieveKnowledge(ctx, query)
	if err != nil {
		s.logger.Errorf("Knowledge retrieval failed: %v", err)
		// 继续处理，使用空文档
		docs = []models.KnowledgeDoc{}
		strategy = "fallback"
	}

	// 构建增强 prompt
	prompt := s.buildEnhancedPrompt(query, docs)

	// 调用 OpenAI
	response, err := s.callOpenAI(ctx, prompt)
	if err != nil {
		s.logger.Errorf("OpenAI call failed: %v", err)
		// 使用降级响应
		response = s.getFallbackResponse(query)
		strategy = "fallback"
	} else {
		s.metrics.SuccessCount++
	}

	duration := time.Since(startTime)
	s.metrics.AverageLatency = (s.metrics.AverageLatency + duration) / 2

	// 构建响应
	enhancedResp := &EnhancedAIResponse{
		AIResponse: &AIResponse{
			Content:    response,
			Source:     "ai",
			Confidence: s.calculateConfidence(docs, strategy),
		},
		Strategy: strategy,
		Duration: duration,
	}

	// 如果使用了外部 provider 兼容路径，添加来源信息
	if strategy == "weknora" || strategy == "hybrid" {
		enhancedResp.Sources = s.convertDocsToSources(docs)
	}

	return enhancedResp, nil
}

// retrieveKnowledge 知识检索（当前为 WeKnora compatibility + 降级）
func (s *EnhancedAIService) retrieveKnowledge(ctx context.Context, query string) ([]models.KnowledgeDoc, string, error) {
	// 尝试当前外部 knowledge provider（legacy 路径仍为 WeKnora compatibility）
	if s.weKnoraEnabled && s.circuitBreaker.Allow() {
		docs, err := s.searchWithWeKnora(ctx, query)
		if err == nil && len(docs) > 0 {
			s.circuitBreaker.OnSuccess()
			s.metrics.KnowledgeProviderUsageCount++
			s.metrics.WeKnoraUsageCount++
			s.metrics.ActiveKnowledgeProvider = "weknora"
			s.logger.Infof("Knowledge provider search succeeded via WeKnora compatibility path, found %d documents", len(docs))
			return docs, "weknora", nil
		}

		if err != nil {
			s.circuitBreaker.OnFailure()
			s.logger.Warnf("Knowledge provider search failed via WeKnora compatibility path: %v", err)
		}
	}

	// 降级到原知识库
	if s.fallbackEnabled {
		s.logger.Info("Using fallback knowledge base")
		docs := s.knowledgeBase.Search(query, 3)
		s.metrics.FallbackUsageCount++
		s.metrics.ActiveKnowledgeProvider = "fallback"
		return docs, "fallback", nil
	}

	s.metrics.ActiveKnowledgeProvider = ""
	return []models.KnowledgeDoc{}, "none", fmt.Errorf("all knowledge sources unavailable")
}

// searchWithWeKnora 使用 WeKnora compatibility provider 搜索
func (s *EnhancedAIService) searchWithWeKnora(ctx context.Context, query string) ([]models.KnowledgeDoc, error) {
	startTime := time.Now()

	searchReq := &weknora.SearchRequest{
		Query:           query,
		KnowledgeBaseID: s.knowledgeBaseID,
		Limit:           5,
		Threshold:       0.7,
		Strategy:        "hybrid", // 使用混合检索策略
	}

	response, err := s.weKnoraClient.SearchKnowledge(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("knowledge provider search error: %w", err)
	}

	s.metrics.WeKnoraLatency = time.Since(startTime)

	if !response.Success {
		return nil, fmt.Errorf("knowledge provider API error: %s", response.Message)
	}

	// 转换为内部格式
	var docs []models.KnowledgeDoc
	for _, result := range response.Data.Results {
		doc := models.KnowledgeDoc{
			// ID会由数据库自动分配，不从外部 provider 的 DocumentID 设置
			Title:    result.Title,
			Content:  result.Content,
			Category: "weknora",
			Tags:     "weknora,search",
		}
		docs = append(docs, doc)
	}

	return docs, nil
}

// buildEnhancedPrompt 构建增强的提示词
func (s *EnhancedAIService) buildEnhancedPrompt(query string, docs []models.KnowledgeDoc) string {
	var prompt strings.Builder

	prompt.WriteString("你是 Servify 智能客服助手，请根据以下知识库信息回答用户问题。\n\n")

	if len(docs) > 0 {
		prompt.WriteString("🔍 相关知识库信息：\n")
		for i, doc := range docs {
			prompt.WriteString(fmt.Sprintf("%d. 📄 %s\n", i+1, doc.Title))
			prompt.WriteString(fmt.Sprintf("   📝 %s\n\n", doc.Content))
		}
	} else {
		prompt.WriteString("ℹ️ 注意：当前没有找到相关的知识库信息，请基于一般常识回答。\n\n")
	}

	prompt.WriteString("📋 回答要求：\n")
	prompt.WriteString("1. ✅ 优先基于知识库信息提供准确回答\n")
	prompt.WriteString("2. 🔍 如果知识库信息不足，请诚实说明并提供一般性建议\n")
	prompt.WriteString("3. 😊 保持友好、专业的语气\n")
	prompt.WriteString("4. 🆘 如果问题超出能力范围，建议转人工客服\n")
	prompt.WriteString("5. 🎯 回答要简洁明了，避免冗长\n\n")

	prompt.WriteString(fmt.Sprintf("❓ 用户问题：%s\n\n", query))
	prompt.WriteString("💬 请用中文回答：")

	return prompt.String()
}

// calculateConfidence 计算置信度
func (s *EnhancedAIService) calculateConfidence(docs []models.KnowledgeDoc, strategy string) float64 {
	baseConfidence := 0.5

	switch strategy {
	case "weknora":
		baseConfidence = 0.8
	case "fallback":
		baseConfidence = 0.6
	case "none":
		baseConfidence = 0.3
	}

	// 根据文档数量调整置信度
	if len(docs) > 0 {
		docBonus := float64(len(docs)) * 0.05
		if docBonus > 0.15 {
			docBonus = 0.15
		}
		baseConfidence += docBonus
	}

	// 确保置信度在合理范围内。基础置信度最小为 0.3（"none" 策略），文档加成
	// 只增不减，下限 0.1 的分支不可达，无需 clamp。
	if baseConfidence > 0.95 {
		baseConfidence = 0.95
	}

	return baseConfidence
}

// convertDocsToSources 转换文档为来源信息
func (s *EnhancedAIService) convertDocsToSources(docs []models.KnowledgeDoc) []weknora.SearchResult {
	var sources []weknora.SearchResult
	for _, doc := range docs {
		source := weknora.SearchResult{
			DocumentID: fmt.Sprintf("%d", doc.ID), // 转换uint为string
			Title:      doc.Title,
			Content:    doc.Content,
			Score:      0.8, // 默认分数
			Source:     "knowledge_base",
		}
		sources = append(sources, source)
	}
	return sources
}

// UploadKnowledgeDocument 上传文档到当前外部 knowledge provider。
func (s *EnhancedAIService) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	if !s.weKnoraEnabled {
		return fmt.Errorf("knowledge provider is not enabled")
	}

	doc := &weknora.Document{
		Type:    "text",
		Title:   title,
		Content: content,
		Tags:    tags,
	}

	_, err := s.weKnoraClient.UploadDocument(ctx, s.knowledgeBaseID, doc)
	if err != nil {
		return fmt.Errorf("failed to upload document to knowledge provider: %w", err)
	}

	s.logger.Infof("Successfully uploaded document '%s' to knowledge provider via WeKnora compatibility path", title)
	return nil
}

// UploadDocumentToWeKnora preserves the legacy method name.
func (s *EnhancedAIService) UploadDocumentToWeKnora(ctx context.Context, title, content string, tags []string) error {
	return s.UploadKnowledgeDocument(ctx, title, content, tags)
}

// GetMetrics 获取服务指标
func (s *EnhancedAIService) GetMetrics() *AIMetrics {
	return s.metrics
}

// GetStatus 获取服务状态
func (s *EnhancedAIService) GetStatus(ctx context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"type":                       "enhanced",
		"knowledge_provider":         s.activeKnowledgeProviderID(),
		"knowledge_provider_enabled": s.weKnoraEnabled,
		"knowledge_mode":             "provider_fallback",
		"fallback_enabled":           s.fallbackEnabled,
		"metrics":                    s.metrics,
	}

	// 检查当前 knowledge provider 健康状态（legacy 路径仍为 WeKnora compatibility）
	if s.weKnoraEnabled {
		if s.weKnoraClient != nil {
			err := s.weKnoraClient.HealthCheck(ctx)
			status["knowledge_provider_healthy"] = err == nil
			if err != nil {
				status["knowledge_provider_error"] = err.Error()
			}
		} else {
			status["knowledge_provider_healthy"] = false
			status["knowledge_provider_error"] = "knowledge provider client not initialized"
		}
	}

	// 熔断器状态
	status["circuit_breaker"] = map[string]interface{}{
		"state":         s.circuitBreaker.State(),
		"failure_count": s.circuitBreaker.FailureCount(),
	}

	return status
}

// SetKnowledgeProviderEnabled 动态开启/关闭外部 knowledge provider。
func (s *EnhancedAIService) SetKnowledgeProviderEnabled(enabled bool) {
	s.weKnoraEnabled = enabled
	s.logger.Infof("Knowledge provider enabled set to: %v", enabled)
}

// SetWeKnoraEnabled preserves the legacy method name.
func (s *EnhancedAIService) SetWeKnoraEnabled(enabled bool) {
	s.SetKnowledgeProviderEnabled(enabled)
}

// SetFallbackEnabled 动态开启/关闭降级
func (s *EnhancedAIService) SetFallbackEnabled(enabled bool) {
	s.fallbackEnabled = enabled
	s.logger.Infof("Fallback enabled set to: %v", enabled)
}

// ResetCircuitBreaker 重置熔断器
func (s *EnhancedAIService) ResetCircuitBreaker() {
	s.circuitBreaker.Reset()
	s.logger.Info("Circuit breaker reset")
}

// SyncKnowledgeBase 同步知识库到当前外部 knowledge provider。
func (s *EnhancedAIService) SyncKnowledgeBase(ctx context.Context) error {
	if !s.weKnoraEnabled {
		return fmt.Errorf("knowledge provider is not enabled")
	}

	s.logger.Info("Starting knowledge base synchronization...")

	// 获取原知识库的所有文档
	docs := s.knowledgeBase.documents
	successCount := 0
	errorCount := 0

	for _, doc := range docs {
		err := s.UploadKnowledgeDocument(ctx, doc.Title, doc.Content, strings.Split(doc.Tags, ","))
		if err != nil {
			s.logger.Errorf("Failed to sync document '%s': %v", doc.Title, err)
			errorCount++
		} else {
			successCount++
		}
	}

	s.logger.Infof("Knowledge base sync completed: %d success, %d errors", successCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("sync completed with %d errors", errorCount)
	}

	return nil
}

func (s *EnhancedAIService) activeKnowledgeProviderID() string {
	if s.weKnoraEnabled {
		return "weknora"
	}
	return ""
}
