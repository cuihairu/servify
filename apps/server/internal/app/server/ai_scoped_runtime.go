package server

import (
	"context"
	"strings"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	svcmetrics "servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/platform/configscope"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type scopedAIRuntimeService struct {
	cfg           *config.Config
	logger        *logrus.Logger
	resolver      *configscope.Resolver
	fallback      aidelivery.RuntimeService
	businessMeter *svcmetrics.BusinessMetrics
}

func NewScopedAIRuntimeService(cfg *config.Config, logger *logrus.Logger, db *gorm.DB, fallback aidelivery.RuntimeService, businessMeter *svcmetrics.BusinessMetrics) aidelivery.RuntimeService {
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
	return &scopedAIRuntimeService{cfg: cfg, logger: logger, resolver: resolver, fallback: fallback, businessMeter: businessMeter}
}

func (s *scopedAIRuntimeService) ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error) {
	service := s.buildService(ctx)
	if service == nil {
		return nil, nil
	}
	return service.ProcessQuery(ctx, query, sessionID)
}

func (s *scopedAIRuntimeService) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	if s == nil || s.fallback == nil {
		return false
	}
	return s.fallback.ShouldTransferToHuman(query, sessionHistory)
}

func (s *scopedAIRuntimeService) GetSessionSummary(messages []models.Message) (string, error) {
	if s == nil || s.fallback == nil {
		return "", nil
	}
	return s.fallback.GetSessionSummary(messages)
}

func (s *scopedAIRuntimeService) GetStatus(ctx context.Context) map[string]interface{} {
	service := s.buildService(ctx)
	if service == nil {
		return nil
	}
	return service.GetStatus(ctx)
}

func (s *scopedAIRuntimeService) buildService(ctx context.Context) aidelivery.RuntimeService {
	if s == nil {
		return nil
	}
	// knowledge.provider=pgvector 是全局配置，请求级重建不认识它；与
	// BuildAIAssembly 的优先级一致——pgvector 声明时用启动装配的全局实例。
	if s.cfg != nil && strings.TrimSpace(s.cfg.Knowledge.Provider) == "pgvector" && s.fallback != nil {
		return s.fallback
	}
	if s.resolver == nil {
		return s.fallback
	}
	openAIConfig := s.resolver.ResolveOpenAI(ctx, nil)
	difyConfig := s.resolver.ResolveDify(ctx, nil)
	weKnoraConfig := s.resolver.ResolveWeKnora(ctx, nil)
	ragFlowConfig := s.resolver.ResolveRagFlow(ctx, nil)
	aiCfg := config.AIConfig{}
	if s.cfg != nil {
		aiCfg = s.cfg.AI
	}
	return runtimeServiceFromResolvedConfig(openAIConfig, difyConfig, ragFlowConfig, weKnoraConfig, aiCfg, s.logger, s.businessMeter)
}
