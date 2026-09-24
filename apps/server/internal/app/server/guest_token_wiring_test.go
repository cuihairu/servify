package server

import (
	"strings"
	"testing"

	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/sirupsen/logrus"

	"servify/apps/server/internal/platform/eventbus"
)

// ---- wireConversationRuntime 的访客 token 装配面（M3 §10 #2 / D6）----

// TestWireConversationRuntime_GuestTokenIssuerWired 锚定签发服务装配：
// 非 nil、空 secret 拒启动（config gate 的装配层兜底第二层）。
func TestWireConversationRuntime_GuestTokenIssuerWired(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	hub := realtimeplatform.NewWebSocketHub()
	if rt.Config.Security.GuestToken.Required {
		t.Fatal("guest_token must default to disabled")
	}
	if _, err := wireConversationRuntime(rt, hub); err != nil {
		t.Fatalf("default config must wire cleanly: %v", err)
	}
	if rt.GuestTokenIssuer == nil {
		t.Fatal("guest token issuer must be wired")
	}
	if hub.CurrentTokenValidator() != nil {
		t.Fatal("validator must stay unset while required is off")
	}

	rt2 := &Runtime{Config: testRouterConfig()}
	rt2.Config.JWT.Secret = "   "
	if _, err := wireConversationRuntime(rt2, realtimeplatform.NewWebSocketHub()); err == nil || !strings.Contains(err.Error(), "guest token issuer") {
		t.Fatalf("expected blank-secret rejection, got %v", err)
	}
}

// TestBuildRuntime_GuestTokenBlankSecretFails 锚定主装配链的兜底传播：
// 空白 jwt.secret 在 wireConversationRuntime 的签发服务构造处拒绝整链启动
// （config gate 之外的装配层第二层 gate）。
func TestBuildRuntime_GuestTokenBlankSecretFails(t *testing.T) {
	cfg := testRouterConfig()
	cfg.JWT.Secret = "  "
	if _, err := BuildRuntime(cfg, logrus.New(), newRuntimeTestDB(t), nil, eventbus.NewInMemoryBus()); err == nil || !strings.Contains(err.Error(), "guest token issuer") {
		t.Fatalf("expected blank-secret rejection, got %v", err)
	}
}

// TestWireConversationRuntime_GuestTokenRequiredSetsValidator 锚定
// required 开启时 hub 挂上校验闭包且校验真实生效（错 token 拒绝）。
func TestWireConversationRuntime_GuestTokenRequiredSetsValidator(t *testing.T) {
	rt := &Runtime{Config: testRouterConfig()}
	rt.Config.Security.GuestToken.Required = true
	hub := realtimeplatform.NewWebSocketHub()
	if _, err := wireConversationRuntime(rt, hub); err != nil {
		t.Fatalf("wire: %v", err)
	}
	validator := hub.CurrentTokenValidator()
	if validator == nil {
		t.Fatal("required must attach a token validator")
	}
	if err := validator("sess-1", "not-a-token"); err == nil {
		t.Fatal("validator must reject malformed tokens")
	}
}
