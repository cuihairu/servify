package server

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/knowledgeprovider"
	difykp "servify/apps/server/internal/platform/knowledgeprovider/dify"
	ragflowkp "servify/apps/server/internal/platform/knowledgeprovider/ragflow"
	weknorakp "servify/apps/server/internal/platform/knowledgeprovider/weknora"
	"servify/apps/server/internal/platform/llm"
	"servify/apps/server/pkg/dify"
	"servify/apps/server/pkg/ragflow"
	"servify/apps/server/pkg/weknora"

	"github.com/sirupsen/logrus"
)

// knowledgeSource 是「选定的知识源」统一视图：启动期 BuildAIAssembly 与请求级
// runtimeServiceFromResolvedConfig 共用同一份选择与构造逻辑（此前两份 if-else
// 已实际漂移：请求级缺 pgvector 分支与健康降级，见 P2-6 第八刀修复记录）。
// dataset/kb id 由各 driver 内部持有（difykp/weknorakp NewProvider 入参），无需上浮。
//
// 运行期外部知识源故障由编排服务的 circuitBreaker 兜底；启动期选择时的健康
// 检查与 require 语义见 knowledgeSourceOptions。真正的降级开关是
// cfg.Fallback.Enabled。
type knowledgeSource struct {
	driver knowledgeprovider.KnowledgeProvider
	id     string // dify / weknora / pgvector；空 = 无外部知识源
}

func (s knowledgeSource) present() bool { return s.driver != nil }

// buildOrchestrated 用选定知识源构造编排服务，并注入出站模型参数
// （ai.provider 对应配置族导出，零值 = provider 侧默认兜底）。业务指标
// 由调用方按需 AttachBusinessMetrics（启动期不挂、请求级挂——保持既有
// 挂载不对称）。
func (s knowledgeSource) buildOrchestrated(base *aidelivery.AIService, llmProvider llm.LLMProvider, params aidelivery.AIRuntimeParams, logger *logrus.Logger) *aidelivery.OrchestratedEnhancedAIService {
	return aidelivery.NewOrchestratedEnhancedAIService(base, llmProvider, s.driver, s.id, logger).
		WithRuntimeParams(params)
}

type knowledgeSourceOptions struct {
	// checkHealth：启动期 true（选源前探测可达性）；请求级 false（纯构造，
	// 不可达 BaseURL 也成功，运行期故障由 circuitBreaker 兜底）。
	checkHealth bool
	// requireHealthy：健康检查失败时启动失败（RequireKnowledgeProviderHealthy /
	// RequireWeKnoraHealthy 的合成语义）。
	requireHealthy bool
	// fallbackEnabled 透传 cfg.Fallback.Enabled：weknora 健康失败且未 require 时，
	// true 降级为无知识源运行，false 启动失败。
	fallbackEnabled bool
	healthTimeout   time.Duration
	logger          *logrus.Logger
}

// selectKnowledgeSource 统一知识源选择链：ragflow（可选健康检查，健康失败
// 降级 dify）→ dify（可选健康检查，健康失败降级 weknora）→ weknora（可选
// 健康检查）。pgvector 不进此函数：它是全局声明 + 进程内 driver（绑 DB 与
// embedding 实例），由调用方直通启动装配的实例。
func selectKnowledgeSource(ragFlowConfig config.RagFlowConfig, difyConfig config.DifyConfig, weKnoraConfig config.WeKnoraConfig, opts knowledgeSourceOptions) (knowledgeSource, error) {
	logger := opts.logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	if ragFlowConfig.Enabled {
		client := ragflow.NewClient(&ragflow.Config{
			BaseURL: ragFlowConfig.BaseURL,
			APIKey:  ragFlowConfig.APIKey,
			Timeout: ragFlowConfig.Timeout,
		})
		ragFlowHealthy := true
		if opts.checkHealth {
			ctx, cancel := context.WithTimeout(context.Background(), opts.healthTimeout)
			defer cancel()
			if err := client.HealthCheck(ctx, ragFlowConfig.DatasetID); err != nil {
				logger.Warnf("RagFlow health check failed: %v", err)
				ragFlowHealthy = false
				// ragflow 失败且 dify/weknora 也未启用时，require 语义直接启动
				// 失败；任一后续源启用则继续走它自己的 require/fallback 语义。
				if !difyConfig.Enabled && !weKnoraConfig.Enabled && opts.requireHealthy {
					return knowledgeSource{}, fmt.Errorf("ragflow health check failed: %w", err)
				}
			}
		}
		if ragFlowHealthy {
			return knowledgeSource{
				driver: ragflowkp.NewProvider(client, ragFlowConfig.DatasetID, ragflowkp.SearchConfig{
					TopK:           ragFlowConfig.Search.TopK,
					ScoreThreshold: ragFlowConfig.Search.ScoreThreshold,
				}),
				id: "ragflow",
			}, nil
		}
	}

	if difyConfig.Enabled {
		client := dify.NewClient(&dify.Config{
			BaseURL: difyConfig.BaseURL,
			APIKey:  difyConfig.APIKey,
			Timeout: difyConfig.Timeout,
		})
		difyHealthy := true
		if opts.checkHealth {
			ctx, cancel := context.WithTimeout(context.Background(), opts.healthTimeout)
			defer cancel()
			if err := client.HealthCheck(ctx, difyConfig.DatasetID); err != nil {
				logger.Warnf("Dify health check failed: %v", err)
				difyHealthy = false
				// dify 失败且 weknora 也未启用时，require 语义直接启动失败；
				// weknora 启用则继续走 weknora 分支（由它自己的 require/fallback 语义兜底）。
				if !weKnoraConfig.Enabled && opts.requireHealthy {
					return knowledgeSource{}, fmt.Errorf("dify health check failed: %w", err)
				}
			}
		}
		if difyHealthy {
			return knowledgeSource{
				driver: difykp.NewProvider(client, difyConfig.DatasetID, difykp.SearchConfig{
					TopK:            difyConfig.Search.TopK,
					ScoreThreshold:  difyConfig.Search.ScoreThreshold,
					SearchMethod:    difyConfig.Search.SearchMethod,
					RerankingEnable: difyConfig.Search.RerankingEnable,
				}),
				id: "dify",
			}, nil
		}
	}

	if !weKnoraConfig.Enabled {
		return knowledgeSource{}, nil
	}
	client := weknora.NewClient(&weknora.Config{
		BaseURL:    weKnoraConfig.BaseURL,
		APIKey:     weKnoraConfig.APIKey,
		TenantID:   weKnoraConfig.TenantID,
		Timeout:    weKnoraConfig.Timeout,
		MaxRetries: weKnoraConfig.MaxRetries,
	}, logger)
	if opts.checkHealth {
		ctx, cancel := context.WithTimeout(context.Background(), opts.healthTimeout)
		defer cancel()
		if err := client.HealthCheck(ctx); err != nil {
			logger.Warnf("WeKnora health check failed: %v", err)
			if opts.requireHealthy {
				return knowledgeSource{}, fmt.Errorf("weknora health check failed: %w", err)
			}
			if !opts.fallbackEnabled {
				return knowledgeSource{}, fmt.Errorf("weknora unavailable and fallback disabled: %w", err)
			}
			return knowledgeSource{}, nil
		}
	}
	return knowledgeSource{
		driver: weknorakp.NewProvider(client, weKnoraConfig.KnowledgeBaseID),
		id:     "weknora",
	}, nil
}

// applyKnowledgeSource 把选定知识源落到 AIAssembly 统一视图字段。
func (a *AIAssembly) applyKnowledgeSource(source knowledgeSource, service aidelivery.HandlerService, runtime aidelivery.RuntimeService) {
	a.KnowledgeDriver = source.driver
	a.KnowledgeProviderID = source.id
	a.KnowledgeProviderHealthy = source.present()
	a.Service = service
	a.RuntimeService = runtime
}
