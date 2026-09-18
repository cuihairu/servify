package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Embedding.OpenAI.BaseURL)
	assert.Equal(t, "text-embedding-3-small", cfg.Embedding.OpenAI.Model)
}

func TestKnowledgeConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()

	// Provider 默认空：pgvector 需显式声明（见 SetDefaults 注释）。
	assert.Empty(t, cfg.Knowledge.Provider)
	assert.Equal(t, 5, cfg.Knowledge.Pgvector.Search.TopK)
	assert.Equal(t, 0.7, cfg.Knowledge.Pgvector.Search.Threshold)
	assert.Equal(t, "semantic", cfg.Knowledge.Pgvector.Search.Strategy)
	assert.Equal(t, 1000, cfg.Knowledge.Pgvector.Indexing.ChunkSize)
	assert.Equal(t, 200, cfg.Knowledge.Pgvector.Indexing.ChunkOverlap)
}

func TestEmbeddingConfigUnmarshal(t *testing.T) {
	v := viper.New()

	// Manually set config values for testing
	v.Set("embedding.provider", "tei")
	v.Set("embedding.tei.base_url", "http://localhost:8080")
	v.Set("embedding.tei.model", "bge-m3")

	cfg := GetDefaultConfig()
	cfg.Embedding.Provider = v.GetString("embedding.provider")
	cfg.Embedding.TEI.BaseURL = v.GetString("embedding.tei.base_url")
	cfg.Embedding.TEI.Model = v.GetString("embedding.tei.model")

	assert.Equal(t, "tei", cfg.Embedding.Provider)
	assert.Equal(t, "http://localhost:8080", cfg.Embedding.TEI.BaseURL)
	assert.Equal(t, "bge-m3", cfg.Embedding.TEI.Model)
}

func TestKnowledgeConfigUnmarshal(t *testing.T) {
	cfg := &Config{
		Knowledge: KnowledgeConfig{
			Provider: "pgvector",
			Pgvector: PgvectorConfig{
				Search: SearchConfig{
					TopK:      10,
					Threshold: 0.8,
					Strategy:  "semantic",
				},
				Indexing: IndexingConfig{
					ChunkSize:    500,
					ChunkOverlap: 100,
				},
			},
		},
	}

	assert.Equal(t, "pgvector", cfg.Knowledge.Provider)
	assert.Equal(t, 10, cfg.Knowledge.Pgvector.Search.TopK)
	assert.Equal(t, 0.8, cfg.Knowledge.Pgvector.Search.Threshold)
	assert.Equal(t, "semantic", cfg.Knowledge.Pgvector.Search.Strategy)
	assert.Equal(t, 500, cfg.Knowledge.Pgvector.Indexing.ChunkSize)
	assert.Equal(t, 100, cfg.Knowledge.Pgvector.Indexing.ChunkOverlap)
}

func TestEmbeddingConfigXinference(t *testing.T) {
	cfg := &Config{
		Embedding: EmbeddingConfig{
			Provider: "xinference",
			Xinference: XinferenceEmbedConfig{
				BaseURL:  "http://localhost:9997",
				ModelUID: "embedding-model",
			},
		},
	}

	assert.Equal(t, "xinference", cfg.Embedding.Provider)
	assert.Equal(t, "http://localhost:9997", cfg.Embedding.Xinference.BaseURL)
	assert.Equal(t, "embedding-model", cfg.Embedding.Xinference.ModelUID)
}

func TestOpenAIEmbedConfig(t *testing.T) {
	cfg := &Config{
		Embedding: EmbeddingConfig{
			Provider: "openai",
			OpenAI: OpenAIEmbedConfig{
				APIKey:  "test-key",
				BaseURL: "https://api.openai.com/v1",
				Model:   "text-embedding-3-large",
			},
		},
	}

	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "test-key", cfg.Embedding.OpenAI.APIKey)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Embedding.OpenAI.BaseURL)
	assert.Equal(t, "text-embedding-3-large", cfg.Embedding.OpenAI.Model)
}

func TestConfigLoadWithEmbeddingAndKnowledge(t *testing.T) {
	// Test that the config loads without errors
	cfg, err := Load()
	require.NoError(t, err)

	// Verify embedding config exists
	assert.NotNil(t, cfg.Embedding)
	assert.NotEmpty(t, cfg.Embedding.Provider)

	// Verify knowledge config exists
	assert.NotNil(t, cfg.Knowledge)
	// Provider 默认留空：pgvector 需在配置文件显式声明（依赖 pg 扩展与
	// embedding 服务），空默认避免劫持显式配置的 dify/weknora。
	assert.Empty(t, cfg.Knowledge.Provider)

	// Verify pgvector config
	assert.NotNil(t, cfg.Knowledge.Pgvector)
	assert.Greater(t, cfg.Knowledge.Pgvector.Search.TopK, 0)
	assert.Greater(t, cfg.Knowledge.Pgvector.Search.Threshold, 0.0)
	assert.Greater(t, cfg.Knowledge.Pgvector.Indexing.ChunkSize, 0)
}
