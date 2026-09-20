package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/configscope"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/knowledgeprovider"
	pgvectorkp "servify/apps/server/internal/platform/knowledgeprovider/pgvector"
	"servify/apps/server/internal/platform/llm/openai"

	"gorm.io/gorm"

	"github.com/sirupsen/logrus"
)

type AIAssemblyOptions struct {
	RequireKnowledgeProviderHealthy bool
	RequireWeKnoraHealthy           bool
	SyncKnowledgeBase               bool
	HealthCheckTimeout              time.Duration
	// DB 是 pgvector 自建知识库所需的主库句柄（knowledge.provider=pgvector 时必填）。
	DB *gorm.DB
}

// AIAssembly 是启动期 AI 装配的统一视图：知识源经 knowledgeSource 门面选择
// 后只暴露 provider 无关字段（driver / id / healthy），provider 特定状态
// （dify dataset id、weknora client 等）由各 driver 内部持有。
type AIAssembly struct {
	Service                  aidelivery.HandlerService
	RuntimeService           aidelivery.RuntimeService
	KnowledgeDriver          knowledgeprovider.KnowledgeProvider
	KnowledgeProviderID      string
	KnowledgeProviderHealthy bool
}

// KnowledgeProvider 返回选定知识源 driver（KnowledgeDocHandler 消费；nil 安全）。
func (a *AIAssembly) KnowledgeProvider(cfg *config.Config) knowledgeprovider.KnowledgeProvider {
	if a == nil {
		return nil
	}
	return a.KnowledgeDriver
}

func BuildAIAssembly(cfg *config.Config, logger *logrus.Logger, opts AIAssemblyOptions) (*AIAssembly, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	resolver := configscope.NewResolver(cfg)
	openAIConfig := resolver.ResolveOpenAI(context.Background(), nil)
	difyConfig := resolver.ResolveDify(context.Background(), nil)
	weKnoraConfig := resolver.ResolveWeKnora(context.Background(), nil)

	baseAI := aidelivery.NewAIService(openAIConfig.APIKey, openAIConfig.BaseURL)
	baseAI.InitializeKnowledgeBase()
	// 基础编排服务（无外部知识源）：无知识源运行与 pgvector 降级路径共用。
	defaultService := knowledgeSource{}.buildOrchestrated(baseAI, openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL), logger)
	assembly := &AIAssembly{
		Service:        aidelivery.NewHandlerServiceAdapter(defaultService),
		RuntimeService: defaultService,
	}

	// pgvector 自建知识库：knowledge.provider=pgvector 时优先于外部 provider，
	// 复用主库连接与迁移已建的 knowledge_docs(vector(1536)) 表。
	if strings.TrimSpace(cfg.Knowledge.Provider) == "pgvector" {
		return buildPgvectorAssembly(baseAI, openAIConfig.APIKey, openAIConfig.BaseURL, cfg, logger, opts, assembly)
	}

	source, err := selectKnowledgeSource(difyConfig, weKnoraConfig, knowledgeSourceOptions{
		checkHealth:     true,
		requireHealthy:  opts.requireKnowledgeProviderHealthy(),
		fallbackEnabled: cfg.Fallback.Enabled,
		healthTimeout:   timeoutForHealthCheck(opts),
		logger:          logger,
	})
	if err != nil {
		return nil, err
	}
	if !source.present() {
		return assembly, nil
	}
	enhanced := source.buildOrchestrated(baseAI, openai.NewProvider(openAIConfig.APIKey, openAIConfig.BaseURL), logger)
	// 知识同步仅在 weknora 驱动下执行（dify/pgvector 的索引由各自平台/迁移管理）。
	if source.id == "weknora" && opts.SyncKnowledgeBase {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer syncCancel()
		if err := enhanced.SyncKnowledgeBase(syncCtx); err != nil {
			logger.Warnf("Knowledge base sync failed: %v", err)
		}
	}
	assembly.applyKnowledgeSource(source, aidelivery.NewHandlerServiceAdapter(enhanced), enhanced)
	return assembly, nil
}

// buildPgvectorAssembly 装配 pgvector 自建知识库驱动；健康检查失败时按
// requireKnowledgeProviderHealthy 决定启动失败或降级为无知识源运行。
func buildPgvectorAssembly(
	baseAI *aidelivery.AIService,
	apiKey, baseURL string,
	cfg *config.Config,
	logger *logrus.Logger,
	opts AIAssemblyOptions,
	fallback *AIAssembly,
) (*AIAssembly, error) {
	if opts.DB == nil {
		if opts.requireKnowledgeProviderHealthy() {
			return nil, fmt.Errorf("knowledge.provider=pgvector requires a database connection")
		}
		logger.Warnf("knowledge.provider=pgvector set but no database handle available; continuing without it")
		return fallback, nil
	}
	emb, err := BuildEmbeddingProviderFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build embedding provider for pgvector: %w", err)
	}
	if emb == nil {
		if opts.requireKnowledgeProviderHealthy() {
			return nil, fmt.Errorf("knowledge.provider=pgvector requires embedding.provider to be configured")
		}
		logger.Warnf("knowledge.provider=pgvector set but embedding.provider is not configured; continuing without it")
		return fallback, nil
	}
	driver := pgvectorkp.NewProvider(opts.DB, emb, pgvectorkp.Config{
		Search: pgvectorkp.SearchConfig{
			TopK:      cfg.Knowledge.Pgvector.Search.TopK,
			Threshold: cfg.Knowledge.Pgvector.Search.Threshold,
			Strategy:  cfg.Knowledge.Pgvector.Search.Strategy,
		},
		Indexing: pgvectorkp.IndexingConfig{
			ChunkSize:    cfg.Knowledge.Pgvector.Indexing.ChunkSize,
			ChunkOverlap: cfg.Knowledge.Pgvector.Indexing.ChunkOverlap,
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), timeoutForHealthCheck(opts))
	defer cancel()
	if err := driver.HealthCheck(ctx); err != nil {
		if opts.requireKnowledgeProviderHealthy() {
			return nil, fmt.Errorf("pgvector health check failed: %w", err)
		}
		logger.Warnf("pgvector health check failed: %v", err)
		return fallback, nil
	}
	source := knowledgeSource{driver: driver, id: "pgvector"}
	enhanced := source.buildOrchestrated(baseAI, openai.NewProvider(apiKey, baseURL), logger)
	fallback.applyKnowledgeSource(source, aidelivery.NewHandlerServiceAdapter(enhanced), enhanced)
	return fallback, nil
}

// BuildEmbeddingProviderFromConfig 按配置构造 embedding provider。bootstrap 的
// BuildEmbeddingProvider 薄委托到这里：app/server 不能反向 import bootstrap，
// 而 pgvector 分支（同包）需要同一份 cfg→factory 的转换逻辑。
func BuildEmbeddingProviderFromConfig(cfg *config.Config) (embedding.Provider, error) {
	if cfg == nil {
		return nil, nil
	}
	provider := strings.TrimSpace(cfg.Embedding.Provider)
	if provider == "" {
		return nil, nil
	}
	embeddingProvider, err := embedding.NewProvider(embedding.FactoryConfig{
		Provider: provider,
		OpenAI: embedding.OpenAIProviderConfig{
			APIKey:  cfg.Embedding.OpenAI.APIKey,
			BaseURL: cfg.Embedding.OpenAI.BaseURL,
			Model:   cfg.Embedding.OpenAI.Model,
		},
		TEI: embedding.TEIProviderConfig{
			BaseURL: cfg.Embedding.TEI.BaseURL,
			Model:   cfg.Embedding.TEI.Model,
		},
		Xinference: embedding.XinferenceProviderConfig{
			BaseURL:  cfg.Embedding.Xinference.BaseURL,
			ModelUID: cfg.Embedding.Xinference.ModelUID,
		},
	})
	if err != nil && provider == "openai" && strings.TrimSpace(cfg.Embedding.OpenAI.APIKey) == "" {
		return nil, nil
	}
	return embeddingProvider, err
}

func timeoutForHealthCheck(opts AIAssemblyOptions) time.Duration {
	if opts.HealthCheckTimeout > 0 {
		return opts.HealthCheckTimeout
	}
	return 10 * time.Second
}

func (o AIAssemblyOptions) requireKnowledgeProviderHealthy() bool {
	return o.RequireKnowledgeProviderHealthy || o.RequireWeKnoraHealthy
}
