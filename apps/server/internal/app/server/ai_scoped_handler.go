package server

import (
	"context"
	"strings"
	"sync"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/configscope"
	difykp "servify/apps/server/internal/platform/knowledgeprovider/dify"
	weknorakp "servify/apps/server/internal/platform/knowledgeprovider/weknora"
	"servify/apps/server/internal/platform/llm/openai"
	"servify/apps/server/internal/services"
	"servify/apps/server/pkg/dify"
	"servify/apps/server/pkg/weknora"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type scopedAIHandlerService struct {
	cfg           *config.Config
	logger        *logrus.Logger
	resolver      *configscope.Resolver
	fallback      aidelivery.HandlerService
	startup       aidelivery.RuntimeService
	businessMeter *svcmetrics.BusinessMetrics

	mu                       sync.RWMutex
	knowledgeProviderEnabled *bool
}

func NewScopedAIHandlerService(cfg *config.Config, logger *logrus.Logger, db *gorm.DB, fallback aidelivery.HandlerService, startup aidelivery.RuntimeService, businessMeter *svcmetrics.BusinessMetrics) aidelivery.HandlerService {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	resolver := configscope.NewResolver(
		cfg,
		configscope.WithTenantOpenAIProvider(configscope.NewGormTenantConfigProvider(db)),
		configscope.WithWorkspaceOpenAIProvider(configscope.NewGormWorkspaceConfigProvider(db)),
		configscope.WithTenantDifyProvider(configscope.NewGormTenantConfigProvider(db)),
		configscope.WithWorkspaceDifyProvider(configscope.NewGormWorkspaceConfigProvider(db)),
		configscope.WithTenantWeKnoraProvider(configscope.NewGormTenantConfigProvider(db)),
		configscope.WithWorkspaceWeKnoraProvider(configscope.NewGormWorkspaceConfigProvider(db)),
	)
	return &scopedAIHandlerService{cfg: cfg, logger: logger, resolver: resolver, fallback: fallback, startup: startup, businessMeter: businessMeter}
}

func (s *scopedAIHandlerService) ProcessQuery(ctx context.Context, query string, sessionID string) (interface{}, error) {
	service := s.buildService(ctx)
	return aidelivery.NewHandlerServiceAdapter(service).ProcessQuery(ctx, query, sessionID)
}

func (s *scopedAIHandlerService) GetStatus(ctx context.Context) map[string]interface{} {
	service := s.buildService(ctx)
	return aidelivery.NewHandlerServiceAdapter(service).GetStatus(ctx)
}

func (s *scopedAIHandlerService) GetMetrics() (*services.AIMetrics, bool) {
	if s == nil || s.fallback == nil {
		return nil, false
	}
	return s.fallback.GetMetrics()
}

func (s *scopedAIHandlerService) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	service := s.buildService(ctx)
	return aidelivery.NewHandlerServiceAdapter(service).UploadKnowledgeDocument(ctx, title, content, tags)
}

func (s *scopedAIHandlerService) SyncKnowledgeBase(ctx context.Context) error {
	service := s.buildService(ctx)
	return aidelivery.NewHandlerServiceAdapter(service).SyncKnowledgeBase(ctx)
}

func (s *scopedAIHandlerService) SetKnowledgeProviderEnabled(enabled bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	s.knowledgeProviderEnabled = &enabled
	s.mu.Unlock()
	if s.fallback == nil {
		return true
	}
	return s.fallback.SetKnowledgeProviderEnabled(enabled)
}

func (s *scopedAIHandlerService) ResetCircuitBreaker() bool {
	if s == nil || s.fallback == nil {
		return false
	}
	return s.fallback.ResetCircuitBreaker()
}

func (s *scopedAIHandlerService) buildService(ctx context.Context) aidelivery.RuntimeService {
	if s == nil {
		return nil
	}
	// knowledge.provider=pgvector 是全局配置（自建知识库，复用主库连接），
	// 请求级重建不认识它；与 BuildAIAssembly 的优先级一致——pgvector 声明时
	// 直接使用启动装配好的全局实例。
	if s.cfg != nil && strings.TrimSpace(s.cfg.Knowledge.Provider) == "pgvector" && s.startup != nil {
		return s.applyRuntimeOverrides(s.startup)
	}
	if s.resolver == nil {
		return s.applyRuntimeOverrides(runtimeServiceFromResolvedConfig(config.OpenAIConfig{}, config.DifyConfig{}, config.WeKnoraConfig{}, s.logger, s.businessMeter))
	}
	openAIConfig := s.resolver.ResolveOpenAI(ctx, nil)
	difyConfig := s.resolver.ResolveDify(ctx, nil)
	weKnoraConfig := s.resolver.ResolveWeKnora(ctx, nil)
	return s.applyRuntimeOverrides(runtimeServiceFromResolvedConfig(openAIConfig, difyConfig, weKnoraConfig, s.logger, s.businessMeter))
}

func runtimeServiceFromResolvedConfig(openAIConfig config.OpenAIConfig, difyConfig config.DifyConfig, weKnoraConfig config.WeKnoraConfig, logger *logrus.Logger, businessMeter *svcmetrics.BusinessMetrics) aidelivery.RuntimeService {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	baseAI := services.NewAIService(openAIConfig.APIKey, openAIConfig.BaseURL)
	baseAI.InitializeKnowledgeBase()
	// AttachBusinessMetrics 把进程级业务指标挂上（nil 安全），AI 请求打点
	// 见 OrchestratedEnhancedAIService.ProcessQueryEnhanced。
	defaultService := services.NewOrchestratedEnhancedAIService(
		baseAI,
		openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL),
		nil,
		"",
		nil,
		"",
		logger,
	).AttachBusinessMetrics(businessMeter)
	if difyConfig.Enabled {
		client := dify.NewClient(&dify.Config{
			BaseURL: difyConfig.BaseURL,
			APIKey:  difyConfig.APIKey,
			Timeout: difyConfig.Timeout,
		})
		return services.NewOrchestratedEnhancedAIService(
			baseAI,
			openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL),
			difykp.NewProvider(client, difyConfig.DatasetID, difykp.SearchConfig{
				TopK:            difyConfig.Search.TopK,
				ScoreThreshold:  difyConfig.Search.ScoreThreshold,
				SearchMethod:    difyConfig.Search.SearchMethod,
				RerankingEnable: difyConfig.Search.RerankingEnable,
			}),
			"dify",
			nil,
			difyConfig.DatasetID,
			logger,
		).AttachBusinessMetrics(businessMeter)
	}
	if !weKnoraConfig.Enabled {
		return defaultService
	}
	client := weknora.NewClient(&weknora.Config{
		BaseURL:    weKnoraConfig.BaseURL,
		APIKey:     weKnoraConfig.APIKey,
		TenantID:   weKnoraConfig.TenantID,
		Timeout:    weKnoraConfig.Timeout,
		MaxRetries: weKnoraConfig.MaxRetries,
	}, logger)
	return services.NewOrchestratedEnhancedAIService(
		baseAI,
		openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL),
		weknorakp.NewProvider(client, weKnoraConfig.KnowledgeBaseID),
		"weknora",
		client,
		weKnoraConfig.KnowledgeBaseID,
		logger,
	).AttachBusinessMetrics(businessMeter)
}

func (s *scopedAIHandlerService) applyRuntimeOverrides(service aidelivery.RuntimeService) aidelivery.RuntimeService {
	if s == nil || service == nil {
		return service
	}
	enhanced, ok := service.(aidelivery.EnhancedRuntimeService)
	if !ok {
		return service
	}
	s.mu.RLock()
	enabled := s.knowledgeProviderEnabled
	s.mu.RUnlock()
	if enabled != nil {
		enhanced.SetKnowledgeProviderEnabled(*enabled)
	}
	return service
}
