package server

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/config"
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
