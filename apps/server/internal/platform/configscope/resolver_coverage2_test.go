package configscope

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/config"
)

type cov2DifyProvider struct {
	value config.DifyConfig
	ok    bool
	err   error
}

func (s cov2DifyProvider) LoadDifyConfig(ctx context.Context) (config.DifyConfig, bool, error) {
	return s.value, s.ok, s.err
}

func TestCoverage2ResolverResolveDifyPrecedence(t *testing.T) {
	resolver := NewResolver(
		&config.Config{
			Dify: config.DifyConfig{
				BaseURL:   "https://dify-system.example",
				APIKey:    "system-key",
				DatasetID: "ds-system",
				Timeout:   1 * time.Second,
			},
		},
		nil,
		WithTenantDifyProvider(cov2DifyProvider{
			ok: true,
			value: config.DifyConfig{
				Enabled:   true,
				APIKey:    "tenant-key",
				DatasetID: "ds-tenant",
				Timeout:   2 * time.Second,
				Search: config.DifySearchConfig{
					TopK:            3,
					ScoreThreshold:  0.3,
					SearchMethod:    "hybrid",
					RerankingEnable: true,
				},
			},
		}),
		WithWorkspaceDifyProvider(cov2DifyProvider{
			ok: true,
			value: config.DifyConfig{
				BaseURL: "https://dify-workspace.example",
			},
		}),
	)

	got := resolver.ResolveDify(context.Background(), &config.DifyConfig{Timeout: 4 * time.Second})

	if !got.Enabled {
		t.Fatalf("enabled = %v want tenant override true", got.Enabled)
	}
	if got.BaseURL != "https://dify-workspace.example" {
		t.Fatalf("base url = %q want workspace override", got.BaseURL)
	}
	if got.APIKey != "tenant-key" {
		t.Fatalf("api key = %q want tenant override", got.APIKey)
	}
	if got.DatasetID != "ds-tenant" {
		t.Fatalf("dataset = %q want tenant override", got.DatasetID)
	}
	if got.Timeout != 4*time.Second {
		t.Fatalf("timeout = %v want runtime override", got.Timeout)
	}
	if got.Search.TopK != 3 || got.Search.ScoreThreshold != 0.3 || got.Search.SearchMethod != "hybrid" || !got.Search.RerankingEnable {
		t.Fatalf("search = %+v want tenant override", got.Search)
	}
}

func TestCoverage2ResolverResolveDifyProviderSkipsOnErrOrMissing(t *testing.T) {
	resolver := NewResolver(
		nil,
		WithTenantDifyProvider(cov2DifyProvider{err: context.Canceled}),
		WithWorkspaceDifyProvider(cov2DifyProvider{ok: false, value: config.DifyConfig{Enabled: true}}),
	)
	got := resolver.ResolveDify(context.Background(), nil)
	if got.Enabled || got.BaseURL != "" || got.APIKey != "" {
		t.Fatalf("dify = %+v want zero value when providers unusable", got)
	}
}

func TestCoverage2ResolverNilReceiver(t *testing.T) {
	var resolver *Resolver

	portal := resolver.ResolvePortal(context.Background(), nil)
	if portal.BrandName != "Servify" || portal.PrimaryColor != "#4299e1" {
		t.Fatalf("portal defaults not applied for nil resolver: %+v", portal)
	}

	openai := resolver.ResolveOpenAI(context.Background(), nil)
	if openai.APIKey != "" || openai.Timeout != 0 {
		t.Fatalf("openai = %+v want zero value", openai)
	}

	dify := resolver.ResolveDify(context.Background(), nil)
	if dify.Enabled || dify.BaseURL != "" {
		t.Fatalf("dify = %+v want zero value", dify)
	}

	weknora := resolver.ResolveWeKnora(context.Background(), nil)
	if weknora.Enabled || weknora.BaseURL != "" {
		t.Fatalf("weknora = %+v want zero value", weknora)
	}

	risk := resolver.ResolveSessionRisk(context.Background(), nil)
	if risk.HighRiskScore != 0 {
		t.Fatalf("session risk = %+v want zero value", risk)
	}
}

func TestCoverage2ResolverResolvePortalAllFields(t *testing.T) {
	resolver := NewResolver(&config.Config{
		Portal: config.PortalConfig{
			BrandName:      "system",
			LogoURL:        "https://system.example/logo.png",
			PrimaryColor:   "#111111",
			SecondaryColor: "#222222",
			DefaultLocale:  "zh-CN",
			Locales:        []string{"zh-CN"},
			SupportEmail:   "system@example.com",
		},
	})
	got := resolver.ResolvePortal(context.Background(), &config.PortalConfig{
		BrandName:      "runtime",
		LogoURL:        "https://runtime.example/logo.png",
		PrimaryColor:   "#333333",
		SecondaryColor: "#444444",
		DefaultLocale:  "en-US",
		Locales:        []string{"en-US", "ja-JP"},
		SupportEmail:   "runtime@example.com",
	})
	if got.BrandName != "runtime" || got.LogoURL != "https://runtime.example/logo.png" ||
		got.PrimaryColor != "#333333" || got.SecondaryColor != "#444444" ||
		got.DefaultLocale != "en-US" || got.SupportEmail != "runtime@example.com" {
		t.Fatalf("portal = %+v want runtime overlay on all fields", got)
	}
	if len(got.Locales) != 2 || got.Locales[0] != "en-US" || got.Locales[1] != "ja-JP" {
		t.Fatalf("locales = %+v want runtime overlay", got.Locales)
	}
}

func TestCoverage2ResolverResolvePortalBlankOverlayKeepsBase(t *testing.T) {
	resolver := NewResolver(&config.Config{
		Portal: config.PortalConfig{
			BrandName:      "system",
			LogoURL:        "https://system.example/logo.png",
			PrimaryColor:   "#111111",
			SecondaryColor: "#222222",
			DefaultLocale:  "zh-CN",
			Locales:        []string{"zh-CN"},
			SupportEmail:   "system@example.com",
		},
	})
	got := resolver.ResolvePortal(context.Background(), &config.PortalConfig{
		BrandName:      "   ",
		LogoURL:        "",
		PrimaryColor:   "  ",
		SecondaryColor: "",
		DefaultLocale:  " ",
		SupportEmail:   "\t",
	})
	if got.BrandName != "system" || got.LogoURL != "https://system.example/logo.png" ||
		got.PrimaryColor != "#111111" || got.SecondaryColor != "#222222" ||
		got.DefaultLocale != "zh-CN" || got.SupportEmail != "system@example.com" {
		t.Fatalf("portal = %+v want system values preserved on blank overlay", got)
	}
	if len(got.Locales) != 1 || got.Locales[0] != "zh-CN" {
		t.Fatalf("locales = %+v want system values preserved", got.Locales)
	}
}

func TestCoverage2ResolverResolveOpenAIFullMerge(t *testing.T) {
	resolver := NewResolver(
		&config.Config{
			AI: config.AIConfig{
				OpenAI: config.OpenAIConfig{
					APIKey:      "system-key",
					BaseURL:     "https://system.example",
					Model:       "gpt-system",
					Temperature: 0.2,
					MaxTokens:   100,
					Timeout:     time.Second,
				},
			},
		},
		WithTenantOpenAIProvider(stubOpenAIProvider{
			ok: true,
			value: config.OpenAIConfig{
				BaseURL:   "https://tenant.example",
				MaxTokens: 200,
			},
		}),
	)
	got := resolver.ResolveOpenAI(context.Background(), &config.OpenAIConfig{Timeout: 3 * time.Second})
	if got.APIKey != "system-key" || got.Model != "gpt-system" || got.Temperature != 0.2 {
		t.Fatalf("openai = %+v want system base", got)
	}
	if got.BaseURL != "https://tenant.example" || got.MaxTokens != 200 || got.Timeout != 3*time.Second {
		t.Fatalf("openai = %+v want merged overrides", got)
	}
}

func TestCoverage2ResolverResolveOpenAIZeroOverlayKeepsBase(t *testing.T) {
	resolver := NewResolver(&config.Config{
		AI: config.AIConfig{
			OpenAI: config.OpenAIConfig{
				APIKey:      "system-key",
				BaseURL:     "https://system.example",
				Model:       "gpt-system",
				Temperature: 0.2,
				MaxTokens:   100,
				Timeout:     time.Second,
			},
		},
	})
	got := resolver.ResolveOpenAI(context.Background(), &config.OpenAIConfig{})
	if got != (config.OpenAIConfig{
		APIKey:      "system-key",
		BaseURL:     "https://system.example",
		Model:       "gpt-system",
		Temperature: 0.2,
		MaxTokens:   100,
		Timeout:     time.Second,
	}) {
		t.Fatalf("openai = %+v want system values on zero overlay", got)
	}
}

func TestCoverage2ResolverResolveWeKnoraFullMerge(t *testing.T) {
	resolver := NewResolver(
		&config.Config{
			WeKnora: config.WeKnoraConfig{
				Timeout:    time.Second,
				MaxRetries: 1,
			},
		},
		WithTenantWeKnoraProvider(stubWeKnoraProvider{
			ok: true,
			value: config.WeKnoraConfig{
				Enabled:         true,
				BaseURL:         "https://wk-tenant.example",
				APIKey:          "tenant-key",
				TenantID:        "tenant-a",
				KnowledgeBaseID: "kb-tenant",
				Timeout:         2 * time.Second,
				MaxRetries:      3,
				Search: config.WeKnoraSearchConfig{
					DefaultLimit:   7,
					ScoreThreshold: 0.4,
					Strategy:       "merged",
				},
				HealthCheck: config.WeKnoraHealthConfig{
					Interval: 30 * time.Second,
					Timeout:  5 * time.Second,
				},
			},
		}),
	)
	got := resolver.ResolveWeKnora(context.Background(), nil)
	want := config.WeKnoraConfig{
		Enabled:         true,
		BaseURL:         "https://wk-tenant.example",
		APIKey:          "tenant-key",
		TenantID:        "tenant-a",
		KnowledgeBaseID: "kb-tenant",
		Timeout:         2 * time.Second,
		MaxRetries:      3,
		Search: config.WeKnoraSearchConfig{
			DefaultLimit:   7,
			ScoreThreshold: 0.4,
			Strategy:       "merged",
		},
		HealthCheck: config.WeKnoraHealthConfig{
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
		},
	}
	if got != want {
		t.Fatalf("weknora = %+v want %+v", got, want)
	}
}

func TestCoverage2ResolverResolveWeKnoraZeroOverlayKeepsBase(t *testing.T) {
	resolver := NewResolver(&config.Config{
		WeKnora: config.WeKnoraConfig{
			Enabled:         true,
			BaseURL:         "https://wk-system.example",
			APIKey:          "system-key",
			TenantID:        "tenant-system",
			KnowledgeBaseID: "kb-system",
			Timeout:         time.Second,
			MaxRetries:      2,
			Search: config.WeKnoraSearchConfig{
				DefaultLimit:   5,
				ScoreThreshold: 0.5,
				Strategy:       "default",
			},
			HealthCheck: config.WeKnoraHealthConfig{
				Interval: time.Minute,
				Timeout:  10 * time.Second,
			},
		},
	},
		WithWorkspaceWeKnoraProvider(stubWeKnoraProvider{err: context.DeadlineExceeded}),
	)
	got := resolver.ResolveWeKnora(context.Background(), &config.WeKnoraConfig{})
	want := config.WeKnoraConfig{
		Enabled:         true,
		BaseURL:         "https://wk-system.example",
		APIKey:          "system-key",
		TenantID:        "tenant-system",
		KnowledgeBaseID: "kb-system",
		Timeout:         time.Second,
		MaxRetries:      2,
		Search: config.WeKnoraSearchConfig{
			DefaultLimit:   5,
			ScoreThreshold: 0.5,
			Strategy:       "default",
		},
		HealthCheck: config.WeKnoraHealthConfig{
			Interval: time.Minute,
			Timeout:  10 * time.Second,
		},
	}
	if got != want {
		t.Fatalf("weknora = %+v want %+v on zero overlay", got, want)
	}
}

func TestCoverage2ResolverResolveSessionRiskFullMerge(t *testing.T) {
	resolver := NewResolver(
		&config.Config{
			Security: config.SecurityConfig{
				SessionRisk: config.SessionRiskPolicyConfig{
					HotRefreshWindowMinutes:    1,
					RecentRefreshWindowMinutes: 2,
					TodayRefreshWindowHours:    3,
					RapidChangeWindowHours:     4,
					StaleActivityWindowDays:    5,
					MultiPublicIPThreshold:     6,
					ManySessionsThreshold:      7,
					HotRefreshFamilyThreshold:  8,
					MediumRiskScore:            9,
					HighRiskScore:              10,
				},
			},
		},
		WithTenantSessionRiskProvider(stubSessionRiskProvider{
			ok: true,
			value: config.SessionRiskPolicyConfig{
				HotRefreshWindowMinutes:    11,
				RecentRefreshWindowMinutes: 12,
				TodayRefreshWindowHours:    13,
				RapidChangeWindowHours:     14,
				StaleActivityWindowDays:    15,
				MultiPublicIPThreshold:     16,
				ManySessionsThreshold:      17,
				HotRefreshFamilyThreshold:  18,
				MediumRiskScore:            19,
				HighRiskScore:              20,
			},
		}),
	)
	got := resolver.ResolveSessionRisk(context.Background(), nil)
	want := config.SessionRiskPolicyConfig{
		HotRefreshWindowMinutes:    11,
		RecentRefreshWindowMinutes: 12,
		TodayRefreshWindowHours:    13,
		RapidChangeWindowHours:     14,
		StaleActivityWindowDays:    15,
		MultiPublicIPThreshold:     16,
		ManySessionsThreshold:      17,
		HotRefreshFamilyThreshold:  18,
		MediumRiskScore:            19,
		HighRiskScore:              20,
	}
	if got != want {
		t.Fatalf("session risk = %+v want %+v", got, want)
	}
}

func TestCoverage2ResolverResolveSessionRiskZeroOverlayKeepsBase(t *testing.T) {
	resolver := NewResolver(
		&config.Config{
			Server: config.ServerConfig{Environment: "staging"},
			Security: config.SecurityConfig{
				SessionRisk: config.SessionRiskPolicyConfig{
					HotRefreshWindowMinutes: 15,
					HighRiskScore:           4,
				},
				SessionRiskProfiles: map[string]config.SessionRiskPolicyConfig{
					"production": {HighRiskScore: 8},
				},
			},
		},
		WithWorkspaceSessionRiskProvider(stubSessionRiskProvider{ok: false}),
	)
	got := resolver.ResolveSessionRisk(context.Background(), &config.SessionRiskPolicyConfig{})
	if got.HotRefreshWindowMinutes != 15 || got.HighRiskScore != 4 {
		t.Fatalf("session risk = %+v want base values when no profile matches environment", got)
	}
}
