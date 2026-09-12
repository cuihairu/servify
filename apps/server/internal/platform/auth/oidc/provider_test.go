package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"golang.org/x/oauth2"
)

// fakeIDP is a minimal OpenID Provider over httptest: discovery + JWKS, with
// tokens signed by a locally generated RSA key.
type fakeIDP struct {
	server   *httptest.Server
	issuer   string
	key      *rsa.PrivateKey
	kid      string
	clientID string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	p := &fakeIDP{key: key, kid: "test-key-1", clientID: "servify-admin"}
	mux := http.NewServeMux()
	// Handlers read p.issuer at request time; it is set right after the
	// httptest server picks its URL.
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		p.writeJSON(w, map[string]string{
			"issuer":                 p.issuer,
			"jwks_uri":               p.issuer + "/jwks.json",
			"authorization_endpoint": p.issuer + "/authorize",
			"token_endpoint":         p.issuer + "/token",
		})
	})
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(bigEndian(key.E))
		p.writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": p.kid, "n": n, "e": e, "alg": "RS256", "use": "sig",
		}}})
	})
	p.server = httptest.NewServer(mux)
	p.issuer = p.server.URL
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeIDP) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// issueToken signs an ID token with the IdP's current key.
func (p *fakeIDP) issueToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": p.kid, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newTestProvider(t *testing.T, idp *fakeIDP) *Provider {
	t.Helper()
	provider, err := NewFromConfig(context.Background(), config.OIDCConfig{
		Enabled:      true,
		Issuer:       idp.issuer,
		ClientID:     idp.clientID,
		ClientSecret: "secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/oidc/callback",
	}, "development")
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}
	return provider
}

func TestNewFromConfigDiscoveryAndVerify(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	authURL := provider.AuthCodeURL("state-1", "nonce-1", "verifier-1")
	for _, part := range []string{
		"state=state-1", "nonce=nonce-1",
		"code_challenge=", "code_challenge_method=S256", "response_type=code",
		idp.issuer + "/authorize",
	} {
		if !strings.Contains(authURL, part) {
			t.Fatalf("auth url missing %q: %s", part, authURL)
		}
	}

	raw := idp.issueToken(t, map[string]any{
		"iss":            idp.issuer,
		"aud":            idp.clientID,
		"sub":            "user-1",
		"email":          "user@example.com",
		"email_verified": true,
		"name":           "Test User",
		"nonce":          "nonce-1",
		"roles":          []string{"admins", "staff"},
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
	})
	claims, err := provider.Verify(context.Background(), raw, "nonce-1")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admins" {
		t.Fatalf("roles = %v, want [admins staff]", claims.Roles)
	}

	// 错误的 nonce 必须拒绝。
	if _, err := provider.Verify(context.Background(), raw, "nonce-2"); err == nil {
		t.Fatal("expected nonce mismatch to fail")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)

	// 用不在 JWKS 里的另一把钥匙签名。
	rogue, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rogue key: %v", err)
	}
	saved := idp.key
	idp.key = rogue
	raw := idp.issueToken(t, map[string]any{
		"iss": idp.issuer, "aud": idp.clientID, "sub": "attacker",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	idp.key = saved

	if _, err := provider.Verify(context.Background(), raw, ""); err == nil {
		t.Fatal("expected bad signature to fail")
	}
}

func TestNewFromConfigValidations(t *testing.T) {
	if _, err := NewFromConfig(context.Background(), config.OIDCConfig{Issuer: " ", ClientID: "c", ClientSecret: "s", RedirectURL: "r"}, "development"); err == nil {
		t.Fatal("empty issuer must fail")
	}
	if _, err := NewFromConfig(context.Background(), config.OIDCConfig{Issuer: "http://x", ClientID: "", ClientSecret: "s", RedirectURL: "r"}, "development"); err == nil {
		t.Fatal("missing client id must fail")
	}
	if _, err := NewFromConfig(context.Background(), config.OIDCConfig{Issuer: "http://x", ClientID: "c", ClientSecret: "s", RedirectURL: "r"}, "production"); err == nil {
		t.Fatal("http issuer must fail in production")
	}
	if _, err := NewFromConfig(context.Background(), config.OIDCConfig{Issuer: "https://unreachable.invalid", ClientID: "c", ClientSecret: "s", RedirectURL: "r"}, "production"); err == nil {
		t.Fatal("unreachable issuer must fail")
	}
}

func TestExchangeReportsFailure(t *testing.T) {
	// token endpoint 返回 500：Exchange 的错误必须传播而不是被吞掉。
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	provider := New(nopVerifier{}, &oauth2.Config{
		ClientID: "c", ClientSecret: "s", RedirectURL: "r",
		Endpoint: oauth2.Endpoint{TokenURL: srv.URL + "/token"},
	}, nil)
	if _, err := provider.Exchange(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("expected exchange against a failing token endpoint to fail")
	}
}

func TestExchangeRequiresIDToken(t *testing.T) {
	// token endpoint 只回 access_token：缺 id_token 必须显式报错。
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "at", "token_type": "bearer"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	provider := New(nopVerifier{}, &oauth2.Config{
		ClientID: "c", ClientSecret: "s", RedirectURL: "r",
		Endpoint: oauth2.Endpoint{TokenURL: srv.URL + "/token"},
	}, nil)
	if _, err := provider.Exchange(context.Background(), "code", "verifier"); err == nil || !strings.Contains(err.Error(), "id_token") {
		t.Fatalf("got %v, want missing id_token error", err)
	}
}

func TestIssuerHost(t *testing.T) {
	idp := newFakeIDP(t)
	provider := newTestProvider(t, idp)
	if provider.IssuerHost() == "" {
		t.Fatal("expected issuer host to be derived from the issuer URL")
	}
}

type nopVerifier struct{}

func (nopVerifier) Verify(context.Context, string, string) (*Claims, error) {
	return &Claims{Subject: "x"}, nil
}

// bigEndian renders an RSA public exponent the way JWKS expects.
func bigEndian(n int) []byte {
	if n == 0 {
		return []byte{0}
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte(n & 0xff)}, out...)
		n >>= 8
	}
	return out
}
