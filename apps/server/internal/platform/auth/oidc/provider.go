// Package oidc implements the server side of the admin OIDC single sign-on
// flow: authorization-code + PKCE against any standard provider (Keycloak,
// Okta, Entra ID, ...), with the ID token verified locally against the
// provider's published keys.
//
// The flow state (state / nonce / PKCE verifier) lives in a short-lived
// HttpOnly cookie owned by handlers.OIDCHandler; this package only talks the
// protocol.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"servify/apps/server/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// DefaultRoleClaims lists the ID token claims inspected for role/group hints
// when config.OIDC.RoleClaims is not set.
var DefaultRoleClaims = []string{"roles", "groups"}

// Claims carries the ID token facts the local login decision needs.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Roles         []string
	Nonce         string
}

// Verifier validates a raw ID token (signature, issuer, audience, expiry) and
// the nonce bound to the authorization request. Interface seam for tests.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken, nonce string) (*Claims, error)
}

// Provider drives the OIDC authorization-code flow for one issuer.
type Provider struct {
	oauth      *oauth2.Config
	verifier   Verifier
	roleClaims []string
	issuerHost string
}

// New wires a provider from explicit parts (test seam). The issuer host falls
// back to the authorization endpoint's host when it is not derivable from a
// config issuer.
func New(verifier Verifier, oauthCfg *oauth2.Config, roleClaims []string) *Provider {
	if len(roleClaims) == 0 {
		roleClaims = DefaultRoleClaims
	}
	issuerHost := ""
	if oauthCfg != nil {
		if u, err := url.Parse(oauthCfg.Endpoint.AuthURL); err == nil {
			issuerHost = u.Host
		}
	}
	return &Provider{
		oauth:      oauthCfg,
		verifier:   verifier,
		roleClaims: roleClaims,
		issuerHost: issuerHost,
	}
}

// NewFromConfig performs discovery against cfg.Issuer and builds the flow
// provider. Production requires an https issuer; the call fails fast so a
// misconfigured SSO cannot start the server half-configured.
func NewFromConfig(ctx context.Context, cfg config.OIDCConfig, environment string) (*Provider, error) {
	issuer := strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	if issuer == "" {
		return nil, errors.New("oidc.issuer is required")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("oidc.client_id and oidc.client_secret are required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("oidc.redirect_url is required")
	}
	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		u, err := url.Parse(issuer)
		if err != nil || u.Scheme != "https" {
			return nil, fmt.Errorf("oidc.issuer must be https in production, got %q", issuer)
		}
	}

	discovered, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", issuer, err)
	}
	verifier := discovered.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	roleClaims := cfg.RoleClaims
	if len(roleClaims) == 0 {
		roleClaims = DefaultRoleClaims
	}

	host := ""
	if u, err := url.Parse(issuer); err == nil {
		host = u.Host
	}

	return &Provider{
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint:     discovered.Endpoint(),
		},
		verifier:   &goOidcVerifier{verifier: verifier, roleClaims: roleClaims},
		roleClaims: roleClaims,
		issuerHost: host,
	}, nil
}

// AuthCodeURL builds the IdP authorization URL with state, nonce and S256 PKCE
// challenge bound to codeVerifier.
func (p *Provider) AuthCodeURL(state, nonce, codeVerifier string) string {
	return p.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	)
}

// Exchange swaps the authorization code for a raw ID token (PKCE verified by
// the IdP).
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier string) (string, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return "", fmt.Errorf("oidc token exchange: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if strings.TrimSpace(rawIDToken) == "" || !ok {
		return "", errors.New("oidc token response did not contain an id_token")
	}
	return rawIDToken, nil
}

// IssuerHost returns the host of the configured issuer (for the status endpoint).
func (p *Provider) IssuerHost() string { return p.issuerHost }

// Verify validates a raw ID token against the issuer's keys and the request
// nonce.
func (p *Provider) Verify(ctx context.Context, rawIDToken, nonce string) (*Claims, error) {
	return p.verifier.Verify(ctx, rawIDToken, nonce)
}

// goOidcVerifier adapts coreos/go-oidc to the local Verifier interface,
// adding the nonce check (go-oidc verifies signature/aud/iss/exp but not
// nonce) and flattening the configured role claims.
type goOidcVerifier struct {
	verifier   *oidc.IDTokenVerifier
	roleClaims []string
}

// oidcTokenClaims 是包级 seam（默认 (*oidc.IDToken).Claims）：go-oidc 的
// Verify 已将 RawClaims 限定为 JSON 对象、sub 限定为字符串，两个解码错误
// 分支生产不可达，测试注入异常载荷以保持防御性传播。
var oidcTokenClaims = func(token *oidc.IDToken, v any) error { return token.Claims(v) }

func (g *goOidcVerifier) Verify(ctx context.Context, rawIDToken, nonce string) (*Claims, error) {
	token, err := g.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	var payload map[string]json.RawMessage
	if err := oidcTokenClaims(token, &payload); err != nil {
		return nil, fmt.Errorf("decode id token claims: %w", err)
	}
	get := func(key string) (string, bool, error) {
		v, ok := payload[key]
		if !ok || string(v) == "null" {
			return "", false, nil
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return "", true, err
		}
		return s, true, nil
	}

	subject, _, err := get("sub")
	if err != nil {
		return nil, fmt.Errorf("decode sub claim: %w", err)
	}
	if subject == "" {
		return nil, errors.New("id token has no sub claim")
	}
	email, _, _ := get("email")
	name, _, _ := get("name")
	tokenNonce, _, _ := get("nonce")
	if nonce != "" && tokenNonce != nonce {
		return nil, errors.New("id token nonce mismatch")
	}

	emailVerified := false
	if v, ok := payload["email_verified"]; ok {
		_ = json.Unmarshal(v, &emailVerified)
	}

	claims := &Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: emailVerified,
		Name:          name,
		Nonce:         tokenNonce,
	}
	for _, claim := range g.roleClaims {
		v, ok := payload[claim]
		if !ok {
			continue
		}
		claims.Roles = append(claims.Roles, parseRoleValues(v)...)
	}
	return claims, nil
}

func parseRoleValues(raw json.RawMessage) []string {
	// Accepts ["a","b"] or "a".
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	return nil
}
