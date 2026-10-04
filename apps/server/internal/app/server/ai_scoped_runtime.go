package server

import (
	"context"
	"fmt"

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
	historyLoader aidelivery.SessionHistoryLoader
	// answerStore 首答持久化（B3-1b §5.3）：WS 作答（单发与流式终帧）
	// 成功后旁路落 ai_answers 并回填 answer_id。
	answerStore aidelivery.AnswerStore
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
	return &scopedAIRuntimeService{cfg: cfg, logger: logger, resolver: resolver, fallback: fallback, businessMeter: businessMeter, answerStore: aidelivery.NewGormAnswerStore(db)}
}

// WithSessionHistory 注入会话历史读取口（多轮上下文）。启动装配在
// conversation service 构建完成后回填；nil 安全，链式风格统一。
func (s *scopedAIRuntimeService) WithSessionHistory(loader aidelivery.SessionHistoryLoader) aidelivery.RuntimeService {
	if s == nil {
		return s
	}
	s.historyLoader = loader
	return s
}

func (s *scopedAIRuntimeService) ProcessQuery(ctx context.Context, query string, sessionID string) (*aidelivery.AIResponse, error) {
	service := s.buildService(ctx)
	if service == nil {
		return nil, nil
	}
	resp, err := service.ProcessQuery(ctx, query, sessionID)
	if err == nil {
		// 首答持久化（B3-1b）：旁路落 ai_answers 并回填 answer_id。
		aidelivery.RecordResponse(ctx, s.answerStore, query, sessionID, resp)
	}
	return resp, err
}

// ProcessQueryStream 流式首答透传：请求级重建的实例具备流式能力时委托；
// 否则显式报错，由调用方（WS hub）回退非流式路径。nil 安全与其他方法同规。
// 终末 Done 事件的完整首答经包装通道旁路落库（B3-1b）。
func (s *scopedAIRuntimeService) ProcessQueryStream(ctx context.Context, query string, sessionID string) (<-chan aidelivery.AIStreamEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("ai streaming unavailable")
	}
	service := s.buildService(ctx)
	streamer, ok := service.(aidelivery.RuntimeQueryStreamer)
	if !ok {
		return nil, fmt.Errorf("ai streaming unavailable for this request scope")
	}
	stream, err := streamer.ProcessQueryStream(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	return aidelivery.RecordingStreamChan(ctx, s.answerStore, query, sessionID, stream), nil
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
	// knowledge.provider=pgvector/local 是全局配置，请求级重建不认识它；与
	// BuildAIAssembly 的优先级一致——声明时用启动装配的全局实例。
	if s.cfg != nil && isGlobalKnowledgeProvider(s.cfg.Knowledge.Provider) && s.fallback != nil {
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
	return runtimeServiceFromResolvedConfig(openAIConfig, difyConfig, ragFlowConfig, weKnoraConfig, aiCfg, s.logger, s.businessMeter, s.historyLoader)
}
