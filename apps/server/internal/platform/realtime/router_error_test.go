package realtime

import (
	"context"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"testing"
)

func TestMessageRouter_External_NoAdapter(t *testing.T) {
	r := NewMessageRouter(aidelivery.NewAIService("", ""), NewWebSocketHub(), nil)
	msg := UnifiedMessage{UserID: "u", Content: "hi", Type: MessageTypeText}
	if err := r.handleExternalPlatformMessage(context.Background(), string(PlatformTelegram), msg); err == nil {
		t.Fatalf("expected error when adapter missing")
	}
}
