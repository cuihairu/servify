package services

import (
	"testing"

	aidelivery "servify/apps/server/internal/modules/ai/delivery"
)

type dummyAdapter struct{ ch chan UnifiedMessage }

func (d *dummyAdapter) SendMessage(chatID, message string) error { return nil }
func (d *dummyAdapter) ReceiveMessage() <-chan UnifiedMessage    { return d.ch }
func (d *dummyAdapter) GetPlatformType() PlatformType            { return PlatformTelegram }
func (d *dummyAdapter) Start() error                             { return nil }
func (d *dummyAdapter) Stop() error                              { return nil }

func TestMessageRouter_GetPlatformStats(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	ai := aidelivery.NewAIService("", "")
	r := NewMessageRouter(ai, hub, nil)

	d := &dummyAdapter{ch: make(chan UnifiedMessage)}
	r.RegisterPlatform("tg", d)

	st := r.GetPlatformStats()
	if st["total_platforms"].(int) != 1 {
		t.Fatalf("expected 1 platform, got %+v", st)
	}
}
