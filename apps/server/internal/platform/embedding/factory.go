package embedding

import (
	"fmt"

	"servify/apps/server/internal/platform/embedding/local"
	"servify/apps/server/internal/platform/embedding/openai"
	"servify/apps/server/internal/platform/embedding/tei"
	"servify/apps/server/internal/platform/embedding/xinference"
)

// FactoryConfig is the configuration for creating an embedding provider
type FactoryConfig struct {
	Provider   string                   `yaml:"provider" json:"provider"`
	OpenAI     OpenAIProviderConfig     `yaml:"openai" json:"openai"`
	TEI        TEIProviderConfig        `yaml:"tei" json:"tei"`
	Xinference XinferenceProviderConfig `yaml:"xinference" json:"xinference"`
	Local      LocalProviderConfig      `yaml:"local" json:"local"`
}

// LocalProviderConfig holds configuration for the zero-dependency local provider
type LocalProviderConfig struct {
	Dimension int `yaml:"dimension" json:"dimension,omitempty"`
}

// OpenAIProviderConfig holds configuration for OpenAI embedding provider
type OpenAIProviderConfig struct {
	APIKey  string `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// TEIProviderConfig holds configuration for TEI embedding provider
type TEIProviderConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// XinferenceProviderConfig holds configuration for Xinference embedding provider
type XinferenceProviderConfig struct {
	BaseURL  string `yaml:"base_url" json:"base_url,omitempty"`
	ModelUID string `yaml:"model_uid" json:"model_uid,omitempty"`
}

// NewProvider creates an embedding provider based on the given configuration
func NewProvider(cfg FactoryConfig) (Provider, error) {
	switch cfg.Provider {
	case "openai":
		if cfg.OpenAI.APIKey == "" {
			return nil, fmt.Errorf("openai provider requires api_key")
		}
		return openai.NewProvider(openai.Config{
			APIKey:  cfg.OpenAI.APIKey,
			BaseURL: cfg.OpenAI.BaseURL,
			Model:   cfg.OpenAI.Model,
		}), nil
	case "tei":
		if cfg.TEI.BaseURL == "" {
			return nil, fmt.Errorf("tei provider requires base_url")
		}
		return tei.NewProvider(tei.Config{
			BaseURL: cfg.TEI.BaseURL,
			Model:   cfg.TEI.Model,
		}), nil
	case "xinference":
		if cfg.Xinference.BaseURL == "" {
			return nil, fmt.Errorf("xinference provider requires base_url")
		}
		if cfg.Xinference.ModelUID == "" {
			return nil, fmt.Errorf("xinference provider requires model_uid")
		}
		return xinference.NewProvider(xinference.Config{
			BaseURL:  cfg.Xinference.BaseURL,
			ModelUID: cfg.Xinference.ModelUID,
		}), nil
	case "local":
		// 零依赖确定性嵌入器：无网络请求，dimension<=0 时取包默认 256。
		return local.NewProvider(local.Config{Dim: cfg.Local.Dimension}), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Provider)
	}
}
