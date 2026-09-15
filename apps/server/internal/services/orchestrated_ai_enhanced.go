package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	aimodule "servify/apps/server/internal/modules/ai/application"
	"servify/apps/server/internal/platform/knowledgeprovider"
	"servify/apps/server/internal/platform/llm"
	baseweknora "servify/apps/server/pkg/weknora"

	"github.com/sirupsen/logrus"
)

// OrchestratedEnhancedAIService keeps the legacy enhanced AI surface while delegating query flow to the AI module.
type OrchestratedEnhancedAIService struct {
	base                     *AIService
	orchestrator             *aimodule.QueryOrchestrator
	llmProvider              llm.LLMProvider
	knowledgeProvider        knowledgeprovider.KnowledgeProvider
	knowledgeProviderID      string
	weKnoraClient            baseweknora.WeKnoraInterface
	knowledgeBaseID          string
	knowledgeProviderEnabled bool
	fallbackEnabled          bool
	circuitBreaker           *CircuitBreaker
	metrics                  *AIMetrics
	logger                   *logrus.Logger
	toolExecutor             *aimodule.ToolExecutor
}

func NewOrchestratedEnhancedAIService(
	base *AIService,
	llmProvider llm.LLMProvider,
	knowledgeProvider knowledgeprovider.KnowledgeProvider,
	knowledgeProviderID string,
	weKnoraClient baseweknora.WeKnoraInterface,
	knowledgeBaseID string,
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
		weKnoraClient:            weKnoraClient,
		knowledgeBaseID:          knowledgeBaseID,
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
	return resp.AIResponse, nil
}

func (s *OrchestratedEnhancedAIService) ProcessQueryEnhanced(ctx context.Context, query string, sessionID string) (*EnhancedAIResponse, error) {
	start := time.Now()
	s.metrics.QueryCount++
	if s.ShouldTransferToHuman(query, nil) {
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
	if err != nil {
		if s.knowledgeProviderEnabled {
			s.circuitBreaker.OnFailure()
		}
		if s.fallbackEnabled {
			fallback, fbErr := s.base.ProcessQuery(ctx, query, sessionID)
			if fbErr != nil {
				return nil, fbErr
			}
			s.logger.Warnf("AI enhanced query failed, served by fallback (strategy=fallback, session_id=%s, error=%v)", sessionID, err)
			s.metrics.FallbackUsageCount++
			s.metrics.AverageLatency = time.Since(start)
			return &EnhancedAIResponse{
				AIResponse: fallback,
				Strategy:   "fallback",
				Duration:   time.Since(start),
			}, nil
		}
		return nil, err
	}
	if s.knowledgeProviderEnabled {
		s.circuitBreaker.OnSuccess()
	}
	s.metrics.SuccessCount++
	s.metrics.AverageLatency = result.Latency
	s.metrics.OpenAILatency = result.Latency

	enhanced := &EnhancedAIResponse{
		AIResponse: &AIResponse{
			Content:    result.Content,
			Confidence: confidenceFromSources(result.Sources),
			Source:     "ai",
		},
		Strategy: "fallback",
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
		s.metrics.FallbackUsageCount++
	}
	if result.TokenUsage != nil {
		enhanced.TokensUsed = result.TokenUsage.TotalTokens
	}
	return enhanced, nil
}

func (s *OrchestratedEnhancedAIService) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	return s.base.ShouldTransferToHuman(query, sessionHistory)
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
