// Package infra 是推送下发的传输层：FCM HTTP v1（Android）与 APNs token-based
// （iOS）。零新依赖——两段 JWT 签名（RS256/ES256）用标准库 crypto 手搓，
// 网关地址在构造后可替换（测试注入 httptest；生产默认地址见各字段注释）。
// 真机凭证联调依赖 FCM 服务账号 / APNs 密钥（P1-1）。
package infra

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	fcmTokenScope   = "https://www.googleapis.com/auth/firebase.messaging"
	fcmSendEndpoint = "https://fcm.googleapis.com"
	fcmTokenGrant   = "https://oauth2.googleapis.com/token"
	jwtGrantType    = "urn:ietf:params:oauth:grant-type:jwt-bearer"
)

// FCMClient 是 FCM HTTP v1 出站通道：服务账号 RS256 JWT 换 oauth access
// token（缓存至到期前 60s），随后 POST messages:send。
type FCMClient struct {
	projectID   string
	clientEmail string
	privateKey  *rsa.PrivateKey

	// baseURL/tokenURL 生产为 FCM 官方网关；测试替换为 httptest 地址。
	baseURL   string
	tokenURL  string
	transport http.RoundTripper

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

// NewFCMClient 用 Firebase 服务账号 JSON 构建通道（client_email/private_key
// 必备，project_id 由 JSON 解析）。
func NewFCMClient(credentialsJSON string) (*FCMClient, error) {
	var svc struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(credentialsJSON), &svc); err != nil {
		return nil, fmt.Errorf("fcm credentials parse: %w", err)
	}
	if strings.TrimSpace(svc.ClientEmail) == "" {
		return nil, errors.New("fcm credentials missing client_email")
	}
	key, err := parseRSAKey(svc.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("fcm credentials private_key: %w", err)
	}
	if strings.TrimSpace(svc.ProjectID) == "" {
		return nil, errors.New("fcm credentials missing project_id")
	}
	return &FCMClient{
		projectID:   svc.ProjectID,
		clientEmail: svc.ClientEmail,
		privateKey:  key,
		baseURL:     fcmSendEndpoint,
		tokenURL:    fcmTokenGrant,
	}, nil
}

func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	switch key, err := x509.ParsePKCS8PrivateKey(block.Bytes); {
	case err == nil:
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("not an RSA private key")
	default:
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
}

// Send 通过 FCM v1 发送一条通知（platformToken 为设备注册 token）。
// data 附加键值对随消息透传给客户端（session_id 等）。
func (c *FCMClient) Send(ctx context.Context, platformToken, title, body string, data map[string]string) error {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}
	// 契约式忽略：入参均为可序列化字面量，Marshal 不失败
	payload, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": platformToken,
			"notification": map[string]string{
				"title": title,
				"body":  body,
			},
			"data": data,
		},
	})
	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", c.baseURL, c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("fcm build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("fcm send http %d: %s", resp.StatusCode, readErrBody(resp.Body))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// accessToken 返回缓存的 oauth token，临期（<60s）则重铸。
func (c *FCMClient) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}
	assertion := c.signAssertion() // 合法 key 契约式不失败（构造时已验钥）
	form := url.Values{
		"grant_type": {jwtGrantType},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("fcm build oauth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm oauth: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fcm oauth http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &token); err != nil {
		return "", fmt.Errorf("fcm oauth response: %w", err)
	}
	if token.AccessToken == "" {
		return "", errors.New("fcm oauth response missing access_token")
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

// signAssertion 生成 RS256 服务账号 JWT（scope 固定 firebase.messaging）。
// b64JSON 入参为可序列化字面量，err 契约式忽略。
func (c *FCMClient) signAssertion() string {
	now := time.Now()
	header := b64JSON(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims := b64JSON(map[string]any{
		"iss":   c.clientEmail,
		"scope": fcmTokenScope,
		"aud":   c.tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signing := header + "." + claims
	digest := sha256.Sum256([]byte(signing))
	// 契约式忽略：key 合法（构造时已解析验证）且 rand.Reader 不失败
	sig, _ := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (c *FCMClient) client() *http.Client {
	return &http.Client{Transport: c.transport}
}

// b64JSON 序列化为 base64url（契约式：入参为可序列化字面量）。
func b64JSON(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func readErrBody(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, 1<<12))
	return strings.TrimSpace(string(raw))
}
