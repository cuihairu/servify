package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"golang.org/x/oauth2"
)

// TestNewFromConfigRequiresRedirectURL 覆盖 redirect_url 缺失的快速失败分支
// (该校验发生在 discovery 之前,不需要真实 IdP)。
func TestNewFromConfigRequiresRedirectURL(t *testing.T) {
	_, err := NewFromConfig(context.Background(), config.OIDCConfig{
		Issuer: "https://idp.example.com", ClientID: "c", ClientSecret: "s",
	}, "development")
	if err == nil || !strings.Contains(err.Error(), "redirect_url") {
		t.Fatalf("err = %v, want redirect_url required", err)
	}
}

// TestExchangeReturnsRawIDToken 覆盖 token 响应带 id_token 时的成功返回分支。
func TestExchangeReturnsRawIDToken(t *testing.T) {
	const wantToken = "header.payload.signature"
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "at",
			"token_type":   "bearer",
			"id_token":     wantToken,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	provider := New(nopVerifier{}, &oauth2.Config{
		ClientID: "c", ClientSecret: "s", RedirectURL: "r",
		Endpoint: oauth2.Endpoint{TokenURL: srv.URL + "/token"},
	}, nil)

	got, err := provider.Exchange(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got != wantToken {
		t.Fatalf("raw id token = %q, want %q", got, wantToken)
	}
}

// TestVerifyMinimalClaimsOmitsOptionals 覆盖 get() 对缺失声明的回落分支:
// 只带必需声明的 token 不应产生 email/name/nonce/roles。
func TestVerifyMinimalClaimsOmitsOptionals(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer,
		"aud": idp.clientID,
		"sub": "minimal-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	claims, err := provider.Verify(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "minimal-user" {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.Email != "" || claims.EmailVerified || claims.Name != "" || claims.Nonce != "" {
		t.Fatalf("optional claims must be zero: %+v", claims)
	}
	if len(claims.Roles) != 0 {
		t.Fatalf("roles = %v, want empty", claims.Roles)
	}
}

// TestVerifyToleratesNonStringOptionalClaim 覆盖 get() 的声明解码失败分支:
// email 为数字时解码报错,但可选声明的错误被吞掉,登录继续(Email 回落空串)。
// (sub 出错即终止的分支经 go-oidc 不可达:非字符串 sub 在 go-oidc 校验器
// 内部就被拒绝,见 provider.go 死分支说明。)
func TestVerifyToleratesNonStringOptionalClaim(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss":   idp.issuer,
		"aud":   idp.clientID,
		"sub":   "user-num-email",
		"email": 456,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	claims, err := provider.Verify(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "user-num-email" || claims.Email != "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

// TestVerifyRejectsEmptySubject 覆盖 sub 为空串的拒绝分支。
func TestVerifyRejectsEmptySubject(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer,
		"aud": idp.clientID,
		"sub": "",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := provider.Verify(context.Background(), raw, ""); err == nil || !strings.Contains(err.Error(), "no sub claim") {
		t.Fatalf("err = %v, want missing sub claim", err)
	}
}

// TestVerifyRoleClaimValueShapes 覆盖 parseRoleValues 的三种取值形态:
// 字符串(单值)、数字(两种解码都失败返回 nil);列表形态由既有测试覆盖。
func TestVerifyRoleClaimValueShapes(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss":    idp.issuer,
		"aud":    idp.clientID,
		"sub":    "role-user",
		"roles":  "solo-role", // 非列表:按单字符串取值
		"groups": 7,           // 既非列表也非字符串:整体忽略
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	claims, err := provider.Verify(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "solo-role" {
		t.Fatalf("roles = %v, want [solo-role]", claims.Roles)
	}
}

// parseRoleValues 直测:数字取值必须返回 nil(兜底分支)。
func TestParseRoleValuesNonStringNonList(t *testing.T) {
	if got := parseRoleValues([]byte("123")); got != nil {
		t.Fatalf("parseRoleValues(123) = %v, want nil", got)
	}
	if got := parseRoleValues([]byte(`["a","b"]`)); len(got) != 2 {
		t.Fatalf("parseRoleValues(list) = %v", got)
	}
}
