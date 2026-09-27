package server

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	localllm "servify/apps/server/internal/platform/embedding/local"
	"servify/apps/server/internal/platform/llm/openai"

	"github.com/sirupsen/logrus"
)

// TestBuildLocalAssemblyBranches 覆盖 local 装配的降级/失败分支：
// 无 DB 句柄、无 embedding 配置、embedding 健康检查失败（不可达地址）。
func TestBuildLocalAssemblyBranches(t *testing.T) {
	logger := logrus.New()
	baseAI := aidelivery.NewAIService("test-key", "http://127.0.0.1:1")
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "local"
	llmP := openai.NewProvider("k", "u")

	// 无 DB 句柄：默认降级（warn 后返回 fallback），require 时启动失败。
	fallback := &AIAssembly{}
	asm, err := buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("nil DB without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "requires a database connection") {
		t.Fatalf("nil DB with require: err=%v", err)
	}

	// 有 DB 但未配置 embedding（默认 openai 无 key → BuildEmbedding 返回 nil）：
	// 同样降级 / require 报错。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	asm, err = buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("nil embedding without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db, RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "requires embedding.provider") {
		t.Fatalf("nil embedding with require: err=%v", err)
	}

	// embedding 指向不可达地址：HealthCheck 失败，降级 / require 报错。
	cfg.Embedding.OpenAI.APIKey = "test-key"
	cfg.Embedding.OpenAI.BaseURL = "http://127.0.0.1:1"
	asm, err = buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("unhealthy local without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildLocalAssembly(baseAI, llmP, aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db, RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "local knowledge engine health check failed") {
		t.Fatalf("unhealthy local with require: err=%v", err)
	}
}

// TestBuildLocalAssemblySuccess 覆盖健康检查通过后的成功装配：sqlite 内存库 +
// embedding.provider=local（零依赖嵌入器，无网络），driver 落地为 local。
func TestBuildLocalAssemblySuccess(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "local"
	cfg.Embedding.Provider = "local"

	fallback := &AIAssembly{}
	asm, err := buildLocalAssembly(aidelivery.NewAIService("test-key", "http://127.0.0.1:1"), openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logrus.New(), AIAssemblyOptions{DB: db}, fallback)
	if err != nil {
		t.Fatalf("buildLocalAssembly() error = %v", err)
	}
	if asm != fallback || !asm.KnowledgeProviderHealthy || asm.KnowledgeProviderID != "local" || asm.KnowledgeDriver == nil {
		t.Fatalf("unexpected assembly: %+v", asm)
	}
	if asm.Service == nil || asm.RuntimeService == nil {
		t.Fatal("expected enhanced handler/runtime services to be wired")
	}

	// BuildAIAssembly 的 local 分支（优先于 dify/weknora）。
	asm2, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{DB: db})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	if asm2.KnowledgeProviderID != "local" || !asm2.KnowledgeProviderHealthy {
		t.Fatalf("BuildAIAssembly local branch: %+v", asm2)
	}
}

// TestBuildLocalAssemblyEmbeddingFactoryError embedding 工厂本身报错（tei 缺
// base_url）时经 buildLocalAssembly 包装返回。
func TestBuildLocalAssemblyEmbeddingFactoryError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "local"
	cfg.Embedding.Provider = "tei"

	_, err = buildLocalAssembly(aidelivery.NewAIService("test-key", "http://127.0.0.1:1"), openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logrus.New(), AIAssemblyOptions{DB: db}, &AIAssembly{})
	if err == nil || !strings.Contains(err.Error(), "build embedding provider for local") {
		t.Fatalf("expected embedding factory error wrap, got %v", err)
	}
}

// TestBuildEmbeddingProviderFromConfigLocal 工厂 local case：默认维度 256、
// 显式维度透传。
func TestBuildEmbeddingProviderFromConfigLocal(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = "local"
	cfg.Embedding.Local.Dimension = 0
	provider, err := BuildEmbeddingProviderFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildEmbeddingProviderFromConfig(local) error = %v", err)
	}
	if provider == nil || provider.Dimension() != localllm.DefaultDim {
		t.Fatalf("local provider = %v, dim %d; want dim %d", provider, provider.Dimension(), localllm.DefaultDim)
	}
	// 显式维度覆盖默认。
	cfg.Embedding.Local.Dimension = 64
	provider2, err := BuildEmbeddingProviderFromConfig(cfg)
	if err != nil || provider2.Dimension() != 64 {
		t.Fatalf("local provider dim override = %v, %v; want 64", provider2.Dimension(), err)
	}
}

// TestIsGlobalKnowledgeProvider pgvector/local 判为全局自建知识源，其余不是。
func TestIsGlobalKnowledgeProvider(t *testing.T) {
	for _, p := range []string{"pgvector", "local", " pgvector ", " local "} {
		if !isGlobalKnowledgeProvider(p) {
			t.Fatalf("isGlobalKnowledgeProvider(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "dify", "weknora", "ragflow"} {
		if isGlobalKnowledgeProvider(p) {
			t.Fatalf("isGlobalKnowledgeProvider(%q) = true, want false", p)
		}
	}
}

// TestLocalKnowledgeQAEndToEnd 全本地真实链路：knowledge.provider=local +
// embedding.provider=local + ai.provider=local——真实摄入（分块+嵌入落库）、
// 真实检索（进程内余弦+校准门）、真实抽取式回答（逐字来自上下文）、
// sources 回填与 strategy 标识，全程零外部依赖零网络。
func TestLocalKnowledgeQAEndToEnd(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.KnowledgeDoc{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "local"
	cfg.Embedding.Provider = "local"
	cfg.AI.Provider = "local"

	asm, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{DB: db})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	enhanced, ok := asm.RuntimeService.(aidelivery.EnhancedRuntimeService)
	if !ok {
		t.Fatalf("RuntimeService %T does not implement EnhancedRuntimeService", asm.RuntimeService)
	}

	ctx := context.Background()
	// 状态面：provider 标识与健康。
	status := enhanced.GetStatus(ctx)
	if status["knowledge_provider"] != "local" || status["knowledge_provider_enabled"] != true {
		t.Fatalf("status = %v, want local provider enabled", status)
	}
	if status["knowledge_provider_healthy"] != true {
		t.Fatalf("status healthy = %v, want true", status["knowledge_provider_healthy"])
	}

	// 摄入：管理面上传（真实分块+嵌入写入 knowledge_docs）。
	if err := enhanced.UploadKnowledgeDocument(ctx, "退货政策",
		"本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。", nil); err != nil {
		t.Fatalf("UploadKnowledgeDocument: %v", err)
	}

	// 检索+问答：查询与文档强词面重叠，越过校准门；回答抽取自知识原文。
	// sessionID 置空（单轮约定）：retriever 把 ConversationID 透传为
	// SearchRequest.KnowledgeID，自建驱动（pgvector/local 同构）按
	// workspace_id 过滤——非空会话 id 会把全局摄入的文档滤没，这是与
	// pgvector 一致的既有语义（见 todo 会话语义缺口附注）。
	resp, err := enhanced.ProcessQueryEnhanced(ctx, "退款将在几个工作日内原路退回", "")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if !strings.Contains(resp.Content, "五个工作日内原路退回") {
		t.Fatalf("content = %q, want extractive answer from knowledge", resp.Content)
	}
	if resp.Strategy != "local" {
		t.Fatalf("strategy = %q, want local (knowledge-backed)", resp.Strategy)
	}
	if len(resp.Sources) == 0 {
		t.Fatal("sources empty, want knowledge hits")
	}
	if resp.Confidence <= 0 {
		t.Fatalf("confidence = %v, want > 0 with sources", resp.Confidence)
	}
}
