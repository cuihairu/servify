package infra

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// testRSAKeyPEM 生成（并缓存）测试用 RSA 私钥（PEM/PKCS8）——RSA-2048
// keygen 达百毫秒级，包级缓存避免每个用例重复生成。
var (
	rsaKeyOnce sync.Once
	rsaKeyPEM  string
	rsaKeyErr  error
)

func testRSAKeyPEM(t *testing.T) string {
	t.Helper()
	rsaKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			rsaKeyErr = err
			return
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			rsaKeyErr = err
			return
		}
		rsaKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	})
	if rsaKeyErr != nil {
		t.Fatalf("rsa keygen: %v", rsaKeyErr)
	}
	return rsaKeyPEM
}

func fcmCredentialsJSON(t *testing.T) (string, string) {
	t.Helper()
	keyPEM := testRSAKeyPEM(t)
	raw, err := json.Marshal(map[string]string{
		"client_email": "push@test-project.iam.gserviceaccount.com",
		"private_key":  keyPEM,
		"project_id":   "test-project",
	})
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	return string(raw), keyPEM
}

// fcmTestClient 构建指向 httptest 的通道：oauth 交换与发送各记一次调用，
// 断言回调可注入。
func fcmTestClient(t *testing.T, oauthStatus, sendStatus int, onSend func(r *http.Request, body string)) *FCMClient {
	t.Helper()
	creds, _ := fcmCredentialsJSON(t)
	client, err := NewFCMClient(creds)
	if err != nil {
		t.Fatalf("NewFCMClient: %v", err)
	}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.PostFormValue("grant_type") != jwtGrantType {
			t.Errorf("unexpected oauth request: %v", err)
		}
		assertion := r.PostFormValue("assertion")
		parts := strings.Split(assertion, ".")
		if len(parts) != 3 {
			t.Errorf("assertion is not a JWT: %q", assertion)
		}
		w.WriteHeader(oauthStatus)
		if oauthStatus == http.StatusOK {
			_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":3600}`))
		} else {
			_, _ = w.Write([]byte("oauth upstream down"))
		}
	}))
	t.Cleanup(tokenSrv.Close)
	client.tokenURL = tokenSrv.URL

	sendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := readErrBody(r.Body)
		if onSend != nil {
			onSend(r, raw)
		}
		w.WriteHeader(sendStatus)
	}))
	t.Cleanup(sendSrv.Close)
	client.baseURL = sendSrv.URL
	return client
}

func TestFCMClientSendSuccess(t *testing.T) {
	var sendCalls atomic.Int32
	client := fcmTestClient(t, http.StatusOK, http.StatusOK, func(r *http.Request, body string) {
		n := sendCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q", got)
		}
		// 第二次发送 data=nil（token 缓存路径），数据面只对首次断言
		if n == 1 && (!strings.Contains(body, `"token":"device-token-1"`) ||
			!strings.Contains(body, `"session_id":"m-1"`) ||
			strings.Contains(body, "null")) {
			t.Errorf("payload = %s", body)
		}
	})
	if err := client.Send(context.Background(), "device-token-1", "新回复", "内容", map[string]string{"session_id": "m-1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// token 缓存：第二次发送不再打 oauth
	if err := client.Send(context.Background(), "device-token-1", "新回复", "内容", nil); err != nil {
		t.Fatalf("Send cached: %v", err)
	}
	if sendCalls.Load() != 2 {
		t.Fatalf("send calls = %d", sendCalls.Load())
	}
}

func TestFCMClientSendHTTPErrors(t *testing.T) {
	client := fcmTestClient(t, http.StatusOK, http.StatusUnauthorized, nil)
	err := client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "fcm send http 401") {
		t.Fatalf("err = %v", err)
	}
}

func TestFCMClientOAuthFailure(t *testing.T) {
	client := fcmTestClient(t, http.StatusServiceUnavailable, http.StatusOK, nil)
	err := client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "fcm oauth http 503") {
		t.Fatalf("err = %v", err)
	}
}

func TestFCMClientSendTransportError(t *testing.T) {
	client := fcmTestClient(t, http.StatusOK, http.StatusOK, nil)
	client.baseURL = "http://127.0.0.1:1" // 连接拒绝
	err := client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "fcm send:") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewFCMClientRejectsBadCredentials(t *testing.T) {
	cases := map[string]string{
		"invalid json":    "{not json",
		"missing email":   `{"private_key":"k","project_id":"p"}`,
		"missing key":     `{"client_email":"a@b","project_id":"p"}`,
		"bad pem":         `{"client_email":"a@b","project_id":"p","private_key":"not pem"}`,
		"missing project": `{"client_email":"a@b","private_key":"` + strings.ReplaceAll(testRSAKeyPEM(t), "\n", `\n`) + `"}`,
	}
	for name, raw := range cases {
		if _, err := NewFCMClient(raw); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestFCMClientMalformedOAuthResponse(t *testing.T) {
	creds, _ := fcmCredentialsJSON(t)
	client, err := NewFCMClient(creds)
	if err != nil {
		t.Fatalf("NewFCMClient: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"no_token":true}`)) // 缺 access_token
	}))
	t.Cleanup(srv.Close)
	client.tokenURL = srv.URL
	err = client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "missing access_token") {
		t.Fatalf("err = %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv2.Close)
	client.accessToken = "" // 清缓存重走 oauth
	client.tokenURL = srv2.URL
	err = client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "fcm oauth response") {
		t.Fatalf("err2 = %v", err)
	}
}

func TestFCMClientBuildRequestFailure(t *testing.T) {
	creds, _ := fcmCredentialsJSON(t)
	client, err := NewFCMClient(creds)
	if err != nil {
		t.Fatalf("NewFCMClient: %v", err)
	}
	// 先让 oauth 经 httptest 成功缓存 token，才可能到达发送请求的构建行
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":3600}`))
	}))
	t.Cleanup(oauth.Close)
	client.tokenURL = oauth.URL
	send := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(send.Close)
	client.baseURL = send.URL
	if err := client.Send(context.Background(), "tok", "t", "b", nil); err != nil {
		t.Fatalf("warm-up: %v", err)
	}
	client.baseURL = "://bad" // NewRequestWithContext 的 URL 解析失败
	if err := client.Send(context.Background(), "tok", "t", "b", nil); err == nil {
		t.Fatal("expected build error")
	}
	// oauth 请求行构建失败：tokenURL 解析非法（缓存已空——新实例）
	client2, _ := NewFCMClient(creds)
	client2.tokenURL = "://bad"
	if err := client2.Send(context.Background(), "tok", "t", "b", nil); err == nil {
		t.Fatal("expected oauth build error")
	}
	// oauth 网关不可达（Do err）：tokenURL 指向已关闭的 httptest
	client3, _ := NewFCMClient(creds)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	client3.tokenURL = deadURL
	err3 := client3.Send(context.Background(), "tok", "t", "b", nil)
	if err3 == nil || !strings.Contains(err3.Error(), "fcm oauth:") {
		t.Fatalf("err3 = %v", err3)
	}
}

func TestParseRSAKeyAcceptsPKCS1AndRejectsNonRSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if _, err := parseRSAKey(pkcs1); err != nil {
		t.Fatalf("pkcs1: %v", err)
	}
	// PKCS8 包装的非 RSA 私钥 → not an RSA private key
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("eckeygen: %v", err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(ecKey)
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	raw := `{"client_email":"a@b","project_id":"p","private_key":"` + strings.ReplaceAll(ecPEM, "\n", `\n`) + `"}`
	if _, err := NewFCMClient(raw); err == nil {
		t.Fatal("expected not-an-RSA error")
	}
}
