package delivery

import (
	"strings"
	"testing"
	"time"
)

// TestGuestTokenServiceIssueAndValidate 闭环：签发的 token 能被同 secret
// 校验器放行、绑定正确 session、过期时间在未来。
func TestGuestTokenServiceIssueAndValidate(t *testing.T) {
	svc, err := NewGuestTokenService("guest-secret-1", time.Hour)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	token, expiresAt, err := svc.IssueGuestToken("sess-g1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if expiresAt <= time.Now().Unix() {
		t.Fatalf("expiresAt %d is not in the future", expiresAt)
	}

	validate := NewGuestTokenValidator("guest-secret-1")
	if err := validate("sess-g1", token); err != nil {
		t.Fatalf("validator rejected own token: %v", err)
	}
}

// TestGuestTokenValidatorRejections 锚定校验拒绝面：跨 session 绑定、
// 畸形 token、错 secret。
func TestGuestTokenValidatorRejections(t *testing.T) {
	svc, err := NewGuestTokenService("guest-secret-1", time.Hour)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	token, _, err := svc.IssueGuestToken("sess-g1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	validate := NewGuestTokenValidator("guest-secret-1")
	if err := validate("sess-other", token); err == nil || !strings.Contains(err.Error(), "different session") {
		t.Fatalf("expected cross-session rejection, got %v", err)
	}
	if err := validate("sess-g1", "tampered"); err == nil {
		t.Fatal("expected malformed token rejection")
	}

	otherValidator := NewGuestTokenValidator("guest-secret-2")
	if err := otherValidator("sess-g1", token); err == nil {
		t.Fatal("expected wrong-secret rejection")
	}
}

// TestNewGuestTokenServiceGuards 锚定装配守卫与 TTL 归一（svc.ttl 为包内
// 字段，同包测试直读）。
func TestNewGuestTokenServiceGuards(t *testing.T) {
	if _, err := NewGuestTokenService("  ", time.Hour); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("expected blank-secret rejection, got %v", err)
	}

	// ttl 非正值归一到 DefaultGuestTokenTTL。
	svc, err := NewGuestTokenService("guest-secret-1", -time.Minute)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if svc.ttl != DefaultGuestTokenTTL {
		t.Fatalf("ttl = %v, want default %v", svc.ttl, DefaultGuestTokenTTL)
	}
}
