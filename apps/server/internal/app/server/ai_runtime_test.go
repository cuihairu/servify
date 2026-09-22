package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/internal/platform/llm/openai"
	"strings"
	"testing"

	"servify/apps/server/internal/config"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestAIAssemblyKnowledgeProviderNilSafety 覆盖 KnowledgeProvider 的 nil 接收者
// 与空视图防御（provider 特定字段已收敛进 knowledgeSource 门面，统一视图仅五字段）。
func TestAIAssemblyKnowledgeProviderNilSafety(t *testing.T) {
	var nilAssembly *AIAssembly
	if provider := nilAssembly.KnowledgeProvider(&config.Config{}); provider != nil {
		t.Fatal("expected nil provider on nil assembly")
	}
	empty := &AIAssembly{}
	if provider := empty.KnowledgeProvider(&config.Config{}); provider != nil {
		t.Fatal("expected nil provider without driver")
	}
}

func TestBuildAIAssemblyPrefersDifyOverWeKnora(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/datasets/ds-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ds-1","name":"Primary Dify Dataset"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	cfg.Dify.Enabled = true
	cfg.Dify.BaseURL = server.URL
	cfg.Dify.APIKey = "dify-key"
	cfg.Dify.DatasetID = "ds-1"
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.BaseURL = "http://127.0.0.1:1"
	cfg.WeKnora.APIKey = "wk-key"

	assembly, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	if assembly.KnowledgeProviderID != "dify" {
		t.Fatalf("knowledge provider = %q", assembly.KnowledgeProviderID)
	}
	if !assembly.KnowledgeProviderHealthy {
		t.Fatalf("expected knowledge provider health to be tracked")
	}
	status := assembly.RuntimeService.GetStatus(context.Background())
	if provider, _ := status["knowledge_provider"].(string); provider != "dify" {
		t.Fatalf("status provider = %q", provider)
	}
}

func TestBuildAIAssemblyPrefersRagFlowOverDifyAndWeKnora(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/datasets" && r.URL.Query().Get("id") == "ds-rf" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"ds-rf","name":"RF KB"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	cfg.RagFlow.Enabled = true
	cfg.RagFlow.BaseURL = server.URL
	cfg.RagFlow.APIKey = "ragflow-key"
	cfg.RagFlow.DatasetID = "ds-rf"
	cfg.Dify.Enabled = true
	cfg.Dify.BaseURL = "http://127.0.0.1:1"
	cfg.Dify.APIKey = "dify-key"
	cfg.Dify.DatasetID = "ds-1"
	cfg.WeKnora.Enabled = true
	cfg.WeKnora.BaseURL = "http://127.0.0.1:1"
	cfg.WeKnora.APIKey = "wk-key"

	assembly, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	if assembly.KnowledgeProviderID != "ragflow" {
		t.Fatalf("knowledge provider = %q", assembly.KnowledgeProviderID)
	}
	if !assembly.KnowledgeProviderHealthy {
		t.Fatalf("expected knowledge provider health to be tracked")
	}
	status := assembly.RuntimeService.GetStatus(context.Background())
	if provider, _ := status["knowledge_provider"].(string); provider != "ragflow" {
		t.Fatalf("status provider = %q", provider)
	}
}

func TestBuildEmbeddingProviderFromConfig(t *testing.T) {
	// nil / 空 provider：不构造（调用方按 nil 判定"未配置 embedding"）。
	if provider, err := BuildEmbeddingProviderFromConfig(nil); provider != nil || err != nil {
		t.Fatalf("nil cfg = (%v, %v), want (nil, nil)", provider, err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Embedding.Provider = ""
	if provider, err := BuildEmbeddingProviderFromConfig(cfg); provider != nil || err != nil {
		t.Fatalf("blank provider = (%v, %v), want (nil, nil)", provider, err)
	}

	// openai 无 key：按既有 bootstrap 语义降级为 nil（不报错）。
	cfg.Embedding.Provider = "openai"
	if provider, err := BuildEmbeddingProviderFromConfig(cfg); provider != nil || err != nil {
		t.Fatalf("openai without key = (%v, %v), want (nil, nil)", provider, err)
	}

	// openai 带 key：正常构造。
	cfg.Embedding.OpenAI.APIKey = "test-key"
	provider, err := BuildEmbeddingProviderFromConfig(cfg)
	if err != nil || provider == nil {
		t.Fatalf("openai with key = (%v, %v), want provider", provider, err)
	}

	// tei 缺 base_url：配置错误必须暴露。
	cfg.Embedding.Provider = "tei"
	if _, err := BuildEmbeddingProviderFromConfig(cfg); err == nil {
		t.Fatal("tei without base_url should fail")
	}
}

func TestBuildPgvectorAssemblyBranches(t *testing.T) {
	logger := logrus.New()
	baseAI := aidelivery.NewAIService("test-key", "http://127.0.0.1:1")
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"

	// 无 DB 句柄：默认降级（warn 后返回 fallback），require 时启动失败。
	fallback := &AIAssembly{}
	asm, err := buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("nil DB without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "requires a database connection") {
		t.Fatalf("nil DB with require: err=%v", err)
	}

	// 有 DB 但未配置 embedding：同样降级 / require 报错。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	asm, err = buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("nil embedding without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db, RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "requires embedding.provider") {
		t.Fatalf("nil embedding with require: err=%v", err)
	}

	// embedding 指向不可达地址：HealthCheck 失败，降级 / require 报错。
	cfg.Embedding.OpenAI.APIKey = "test-key"
	cfg.Embedding.OpenAI.BaseURL = "http://127.0.0.1:1"
	asm, err = buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db}, fallback)
	if err != nil || asm != fallback || asm.KnowledgeDriver != nil {
		t.Fatalf("unhealthy pgvector without require: asm=%v err=%v", asm, err)
	}
	if _, err := buildPgvectorAssembly(baseAI, openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logger, AIAssemblyOptions{DB: db, RequireKnowledgeProviderHealthy: true}, &AIAssembly{}); err == nil || !strings.Contains(err.Error(), "pgvector health check failed") {
		t.Fatalf("unhealthy pgvector with require: err=%v", err)
	}
}

// TestBuildPgvectorAssemblySuccess 覆盖健康检查通过后的成功装配路径：
// sqlite 内存库插桩 pg_extension 表供 driver.HealthCheck 的扩展探针查询，
// embedding provider 指向本地 httptest mock（openai 兼容探针任意路径 2xx）。
func TestBuildPgvectorAssemblySuccess(t *testing.T) {
	embServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer embServer.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("CREATE TABLE pg_extension (extname text, extversion text)").Error; err != nil {
		t.Fatalf("stub pg_extension: %v", err)
	}
	if err := db.Exec("INSERT INTO pg_extension VALUES ('vector', '0.8.6')").Error; err != nil {
		t.Fatalf("seed pg_extension: %v", err)
	}

	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	cfg.Embedding.OpenAI.APIKey = "test-key"
	cfg.Embedding.OpenAI.BaseURL = embServer.URL

	fallback := &AIAssembly{}
	asm, err := buildPgvectorAssembly(aidelivery.NewAIService("test-key", "http://127.0.0.1:1"), openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logrus.New(), AIAssemblyOptions{DB: db}, fallback)
	if err != nil {
		t.Fatalf("buildPgvectorAssembly() error = %v", err)
	}
	if asm != fallback || !asm.KnowledgeProviderHealthy || asm.KnowledgeProviderID != "pgvector" || asm.KnowledgeDriver == nil {
		t.Fatalf("unexpected assembly: %+v", asm)
	}
	if asm.Service == nil || asm.RuntimeService == nil {
		t.Fatal("expected enhanced handler/runtime services to be wired")
	}

	// BuildAIAssembly 的 pgvector 分支（优先于 dify/weknora）。
	asm2, err := BuildAIAssembly(cfg, logrus.New(), AIAssemblyOptions{DB: db})
	if err != nil {
		t.Fatalf("BuildAIAssembly() error = %v", err)
	}
	if asm2.KnowledgeProviderID != "pgvector" || !asm2.KnowledgeProviderHealthy {
		t.Fatalf("BuildAIAssembly pgvector branch: %+v", asm2)
	}
}

// embedding 工厂本身报错（tei 缺 base_url）时经 buildPgvectorAssembly 包装返回。
func TestBuildPgvectorAssemblyEmbeddingFactoryError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	cfg.Embedding.Provider = "tei"

	_, err = buildPgvectorAssembly(aidelivery.NewAIService("test-key", "http://127.0.0.1:1"), openai.NewProvider("k", "u"), aidelivery.AIRuntimeParams{}, cfg, logrus.New(), AIAssemblyOptions{DB: db}, &AIAssembly{})
	if err == nil || !strings.Contains(err.Error(), "build embedding provider for pgvector") {
		t.Fatalf("expected embedding factory error wrap, got %v", err)
	}
}
