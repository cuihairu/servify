package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// stubOidcTokenClaims 临时替换 oidcTokenClaims seam，测试结束恢复默认。
func stubOidcTokenClaims(t *testing.T, replacement func(*oidc.IDToken, any) error) {
	t.Helper()
	previous := oidcTokenClaims
	oidcTokenClaims = replacement
	t.Cleanup(func() { oidcTokenClaims = previous })
}

// TestVerifyPropagatesClaimsDecodeError 覆盖 token.Claims 的错误分支：
// go-oidc 的 Verify 已把 RawClaims 限定为 JSON 对象，生产不可达，经 seam 注入。
func TestVerifyPropagatesClaimsDecodeError(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer,
		"aud": idp.clientID,
		"sub": "claims-error-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	claimsErr := errors.New("injected claims failure")
	stubOidcTokenClaims(t, func(*oidc.IDToken, any) error { return claimsErr })

	_, err := provider.Verify(context.Background(), raw, "")
	if err == nil || !strings.Contains(err.Error(), "decode id token claims") {
		t.Fatalf("Verify() error = %v, want claims decode failure", err)
	}
	if !errors.Is(err, claimsErr) {
		t.Fatalf("Verify() error = %v, want wrapped injected error", err)
	}
}

// TestVerifyRejectsNonStringSubject 覆盖 get("sub") 解码失败的终止分支：
// 非字符串 sub 在 go-oidc 校验器内部即被拒绝，只能经 seam 注入异常载荷。
func TestVerifyRejectsNonStringSubject(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer,
		"aud": idp.clientID,
		"sub": "subject-type-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	stubOidcTokenClaims(t, func(_ *oidc.IDToken, v any) error {
		payload, ok := v.(*map[string]json.RawMessage)
		if !ok {
			t.Fatalf("claims target type = %T, want *map[string]json.RawMessage", v)
		}
		*payload = map[string]json.RawMessage{"sub": json.RawMessage("42")}
		return nil
	})

	_, err := provider.Verify(context.Background(), raw, "")
	if err == nil || !strings.Contains(err.Error(), "decode sub claim") {
		t.Fatalf("Verify() error = %v, want sub decode failure", err)
	}
}

// TestVerifyDefaultClaimsPathSucceeds 保证 seam 默认路径行为不变。
func TestVerifyDefaultClaimsPathSucceeds(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer,
		"aud": idp.clientID,
		"sub": "default-path-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	claims, err := provider.Verify(context.Background(), raw, "")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Subject != "default-path-user" {
		t.Fatalf("subject = %q, want default-path-user", claims.Subject)
	}
}
