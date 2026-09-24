package infra

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// testECKeyPEM 生成（并缓存）测试用 ES256 私钥（.p8 全文同款 PKCS8 PEM）。
var (
	ecKeyOnce sync.Once
	ecKeyPEM  string
)

func testECKeyPEM(t *testing.T) string {
	t.Helper()
	ecKeyOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa keygen: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		ecKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	})
	return ecKeyPEM
}

func newAPNsTestClient(t *testing.T, status int, onSend func(r *http.Request, body string)) (*APNsClient, *httptest.Server) {
	t.Helper()
	client, err := NewAPNsClient("KEYID123", "TEAM-9", "com.example.app", testECKeyPEM(t), false)
	if err != nil {
		t.Fatalf("NewAPNsClient: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onSend != nil {
			onSend(r, readErrBody(r.Body))
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	client.host = srv.URL
	return client, srv
}

func TestAPNsClientSendSuccess(t *testing.T) {
	client, _ := newAPNsTestClient(t, http.StatusOK, func(r *http.Request, body string) {
		if got := r.URL.Path; got != "/3/device/hex-token-1" {
			t.Errorf("path = %s", got)
		}
		if !strings.HasPrefix(r.Header.Get("authorization"), "bearer ") {
			t.Errorf("authorization missing bearer")
		}
		if r.Header.Get("apns-topic") != "com.example.app" || r.Header.Get("apns-push-type") != "alert" {
			t.Errorf("apns headers = %v / %v", r.Header.Get("apns-topic"), r.Header.Get("apns-push-type"))
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if _, ok := payload["aps"]; !ok {
			t.Errorf("payload missing aps: %s", body)
		}
		if payload["session_id"] != "m-1" {
			t.Errorf("session_id = %v", payload["session_id"])
		}
	})
	if err := client.Send(context.Background(), "hex-token-1", "新回复", "内容", map[string]string{"session_id": "m-1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestAPNsClientSendHTTPError(t *testing.T) {
	client, _ := newAPNsTestClient(t, http.StatusBadGateway, nil)
	err := client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "apns send http 502") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPNsClientSendTransportError(t *testing.T) {
	client, _ := newAPNsTestClient(t, http.StatusOK, nil)
	client.host = "http://127.0.0.1:1" // 连接拒绝
	if err := client.Send(context.Background(), "tok", "t", "b", nil); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestNewAPNsClientRejectsBadInput(t *testing.T) {
	ecPEM := testECKeyPEM(t)
	rsaPEM := testRSAKeyPEM(t)
	cases := []struct {
		name                             string
		keyID, teamID, bundleID, privKey string
	}{
		{"missing key_id", " ", "t", "b", ecPEM},
		{"missing team_id", "k", "", "b", ecPEM},
		{"missing bundle_id", "k", "t", " ", ecPEM},
		{"missing private_key", "k", "t", "b", ""},
		{"no pem", "k", "t", "b", "not pem"},
		{"not ec key", "k", "t", "b", rsaPEM},
	}
	for _, tc := range cases {
		if _, err := NewAPNsClient(tc.keyID, tc.teamID, tc.bundleID, tc.privKey, false); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestNewAPNsClientSandboxHost(t *testing.T) {
	client, err := NewAPNsClient("k", "t", "b", testECKeyPEM(t), true)
	if err != nil {
		t.Fatalf("NewAPNsClient: %v", err)
	}
	if client.host != apnsSandboxHost {
		t.Fatalf("host = %s", client.host)
	}
}

func TestAPNsClientBuildRequestFailure(t *testing.T) {
	client, _ := newAPNsTestClient(t, http.StatusOK, nil)
	client.host = "://bad" // NewRequestWithContext URL 解析失败
	err := client.Send(context.Background(), "tok", "t", "b", nil)
	if err == nil || !strings.Contains(err.Error(), "apns build request") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewAPNsClientRejectsMalformedDER(t *testing.T) {
	// PEM 头合法但 DER 垃圾 → ParsePKCS8 err
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk der")})
	if _, err := NewAPNsClient("k", "t", "b", string(block), false); err == nil {
		t.Fatal("expected pkcs8 parse error")
	}
}
