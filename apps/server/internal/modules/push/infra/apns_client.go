package infra

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	apnsProductionHost = "https://api.push.apple.com"
	apnsSandboxHost    = "https://api.sandbox.push.apple.com"
)

// APNsClient 是 iOS 出站通道（APNs token-based）：每请求签 ES256 JWT
// （provider token 一小时有效，逐请求生成实现最简、无过期竞态）。
type APNsClient struct {
	keyID    string
	teamID   string
	bundleID string
	key      *ecdsa.PrivateKey

	// host 生产为 APNs 官方网关（Sandbox 选沙箱域）；测试替换 httptest 地址。
	host      string
	transport http.RoundTripper
	nowFn     func() time.Time // 测试注入时钟
}

// NewAPNsClient 用 .p8 私钥全文构建通道；sandbox 走沙箱网关。
func NewAPNsClient(keyID, teamID, bundleID, privateKeyPEM string, sandbox bool) (*APNsClient, error) {
	for name, val := range map[string]string{
		"key_id": keyID, "team_id": teamID, "bundle_id": bundleID, "private_key": privateKeyPEM,
	} {
		if strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("apns missing %s", name)
		}
	}
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("apns private_key: no PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns private_key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns private_key: not an EC key")
	}
	host := apnsProductionHost
	if sandbox {
		host = apnsSandboxHost
	}
	return &APNsClient{
		keyID: keyID, teamID: teamID, bundleID: bundleID,
		key: ecKey, host: host, nowFn: time.Now,
	}, nil
}

// Send 发送一条提醒通知（deviceToken 为 APNs hex token）。
// data 附加键值对并入顶层 payload（session_id 等，aps 之外）。
func (c *APNsClient) Send(ctx context.Context, deviceToken, title, body string, data map[string]string) error {
	auth := c.providerToken() // 契约式不失败（key 合法，构造时已验）
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{"title": title, "body": body},
			"sound": "default",
		},
	}
	for k, v := range data {
		payload[k] = v
	}
	// 契约式忽略：入参均为可序列化字面量，Marshal 不失败
	raw, _ := json.Marshal(payload)
	endpoint := c.host + "/3/device/" + deviceToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("apns build request: %w", err)
	}
	req.Header.Set("authorization", "bearer "+auth)
	req.Header.Set("apns-topic", c.bundleID)
	req.Header.Set("apns-push-type", "alert")

	resp, err := (&http.Client{Transport: c.transport}).Do(req)
	if err != nil {
		return fmt.Errorf("apns send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("apns send http %d: %s", resp.StatusCode, readErrBody(resp.Body))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// providerToken 生成 ES256 签名的 APNs provider JWT（b64JSON 契约式忽略 err）。
func (c *APNsClient) providerToken() string {
	header := b64JSON(map[string]string{"alg": "ES256", "kid": c.keyID})
	claims := b64JSON(map[string]any{"iss": c.teamID, "iat": c.nowFn().Unix()})
	signing := header + "." + claims
	hash := sha256.Sum256([]byte(signing))
	// 契约式忽略：key 合法（构造时已解析验证）且 rand.Reader 不失败
	r, s, _ := ecdsa.Sign(rand.Reader, c.key, hash[:])
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}
