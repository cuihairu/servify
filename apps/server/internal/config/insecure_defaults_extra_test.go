package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInsecureDefaults_S3AndOIDCWarnings(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Upload.Provider = "s3"
	cfg.Upload.S3.Bucket = "  "
	cfg.Upload.S3.Region = ""
	cfg.OIDC.Enabled = true
	cfg.OIDC.Issuer = ""
	cfg.OIDC.ClientID = ""
	cfg.OIDC.ClientSecret = ""
	cfg.OIDC.RedirectURL = ""
	cfg.OIDC.FrontendBaseURL = ""
	cfg.OIDC.AutoProvision = true
	cfg.OIDC.AllowedDomains = nil

	warnings := InsecureDefaults(cfg)

	wantContains := []string{
		"upload.s3.bucket is empty",
		"upload.s3.region is empty",
		"oidc.issuer is empty",
		"oidc.client_id/oidc.client_secret is empty",
		"oidc.redirect_url is empty",
		"oidc.frontend_base_url is empty",
		"oidc.auto_provision",
	}
	for _, want := range wantContains {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("warnings %v missing %q", warnings, want)
		}
	}
}

func TestInsecureDefaults_OIDCHTTPIssuerWarned(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.JWT.Secret = "a-very-long-production-secret-value"
	cfg.Database.Password = "prod-db-password"
	cfg.Upload.Provider = "S3" // 大小写不敏感，且 bucket/region 均已配置
	cfg.Upload.S3.Bucket = "bucket"
	cfg.Upload.S3.Region = "us-east-1"
	cfg.OIDC.Enabled = true
	cfg.OIDC.Issuer = "http://issuer.example.com"
	cfg.OIDC.ClientID = "client"
	cfg.OIDC.ClientSecret = "secret"
	cfg.OIDC.RedirectURL = "https://app.example.com/callback"
	cfg.OIDC.FrontendBaseURL = "https://app.example.com"
	cfg.OIDC.AutoProvision = true
	cfg.OIDC.AllowedDomains = []string{"example.com"}

	warnings := InsecureDefaults(cfg)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "oidc.issuer uses http") {
		t.Fatalf("warnings = %v, want only http-issuer warning", warnings)
	}
}

func TestExpandEnvPlaceholdersNilInterfaceField(t *testing.T) {
	type holder struct {
		V interface{}
	}
	h := holder{}
	expandEnvPlaceholders(reflect.ValueOf(&h)) // struct 字段持 nil interface 必须直接返回

	// map 值为 nil interface：接口分支短路，条目必须原样保留
	m := map[string]interface{}{"k": nil, "v": "${HOME}"}
	expandEnvPlaceholders(reflect.ValueOf(&m).Elem())
	if m["k"] != nil {
		t.Fatalf("nil map entry changed: %v", m["k"])
	}
	if got, ok := m["v"].(string); !ok || got != "${HOME}" {
		t.Fatalf("non-nil map entry unexpectedly rewritten: %v", m["v"])
	}
}

func TestLoadWithResult_UnmarshalError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	// 字符串无法解码进嵌套 struct，触发 viper.Unmarshal 失败路径
	viper.Set("jwt", "not-a-struct")

	cfg, result, err := LoadWithResult()
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal config") {
		t.Fatalf("error = %v, want wrapped unmarshal error", err)
	}
	if cfg != nil {
		t.Fatalf("cfg = %+v, want nil", cfg)
	}
	if result.Valid {
		t.Fatalf("result = %+v, want zero value", result)
	}
}

func TestInitLoggerFallbackFormat(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Log = LogConfig{Level: "info", Format: "xml", Output: "stdout"}
	if err := InitLogger(cfg); err != nil {
		t.Fatalf("InitLogger(unknown format) error = %v", err)
	}
}
