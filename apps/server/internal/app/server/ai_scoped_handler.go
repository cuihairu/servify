package server

import (
	"context"
	"strings"
	"sync"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/configscope"
	llmfactory "servify/apps/server/internal/platform/llm/factory"
	"servify/apps/server/internal/platform/llm/openai"

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
		configscope.WithTenantRagFlowProvider(configscope.NewGormTenantConfigProvider(db)),
		configscope.WithWorkspaceRagFlowProvider(configscope.NewGormWorkspaceConfigProvider(db)),
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

func (s *scopedAIHandlerService) GetMetrics() (*aidelivery.AIMetrics, bool) {
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
		return s.applyRuntimeOverrides(runtimeServiceFromResolvedConfig(config.OpenAIConfig{}, config.DifyConfig{}, config.RagFlowConfig{}, config.WeKnoraConfig{}, s.aiConfig(), s.logger, s.businessMeter))
	}
	openAIConfig := s.resolver.ResolveOpenAI(ctx, nil)
	difyConfig := s.resolver.ResolveDify(ctx, nil)
	weKnoraConfig := s.resolver.ResolveWeKnora(ctx, nil)
	ragFlowConfig := s.resolver.ResolveRagFlow(ctx, nil)
	return s.applyRuntimeOverrides(runtimeServiceFromResolvedConfig(openAIConfig, difyConfig, ragFlowConfig, weKnoraConfig, s.aiConfig(), s.logger, s.businessMeter))
}

// aiConfig 返回全局 AI 配置段（provider 选型 + anthropic 参数族）；
// nil cfg 安全（测试路径），退回零值 = openai 默认行为。
func (s *scopedAIHandlerService) aiConfig() config.AIConfig {
	if s == nil || s.cfg == nil {
		return config.AIConfig{}
	}
	return s.cfg.AI
}

// runtimeServiceFromResolvedConfig 按解析后的租户/工作区配置重建编排服务。
// 知识源选择与启动期 BuildAIAssembly 共用 selectKnowledgeSource 门面，但
// checkHealth=false：请求级不做健康探测（不可达 BaseURL 也纯构造），运行期
// 外部知识源故障由编排服务的 circuitBreaker 兜底。aiCfg 只取全局面
// （provider 选型 + anthropic 参数），openai 参数面来自作用域解析结果。
func runtimeServiceFromResolvedConfig(openAIConfig config.OpenAIConfig, difyConfig config.DifyConfig, ragFlowConfig config.RagFlowConfig, weKnoraConfig config.WeKnoraConfig, aiCfg config.AIConfig, logger *logrus.Logger, businessMeter *svcmetrics.BusinessMetrics) aidelivery.RuntimeService {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	// checkHealth=false 时选择链按契约不产生错误（错误路径全部位于健康检查内），
	// 请求级重建不做健康探测，运行期故障由编排服务 circuitBreaker 兜底。
	source, _ := selectKnowledgeSource(ragFlowConfig, difyConfig, weKnoraConfig, knowledgeSourceOptions{
		checkHealth: false,
		logger:      logger,
	})
	baseAI := aidelivery.NewAIService(openAIConfig.APIKey, openAIConfig.BaseURL)
	baseAI.InitializeKnowledgeBase()
	factoryCfg := llmfactory.Config{
		Provider:  aiCfg.Provider,
		OpenAI:    openAIConfig,
		Anthropic: aiCfg.Anthropic,
	}
	// 选型经 config 层与启动装配双层 gate 后必为合法值；异常时退回
	// openai 直连（与历史行为一致）而不是让请求级重建整体失败。
	llmProvider, err := llmfactory.New(factoryCfg)
	if err != nil {
		logger.Warnf("scoped AI rebuild: %v; falling back to openai provider", err)
		llmProvider = openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL)
	}
	model, temperature, maxTokens, timeoutMs := llmfactory.RuntimeParams(factoryCfg)
	// AttachBusinessMetrics 把进程级业务指标挂上（nil 安全），AI 请求打点
	// 见 OrchestratedEnhancedAIService.ProcessQueryEnhanced。
	return source.buildOrchestrated(baseAI, llmProvider, aidelivery.AIRuntimeParams{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		TimeoutMs:   timeoutMs,
	}, logger).
		AttachBusinessMetrics(businessMeter)
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
