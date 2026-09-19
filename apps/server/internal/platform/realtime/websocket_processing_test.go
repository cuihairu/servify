package realtime

import (
	"testing"
	"time"
)

func TestProcessMessageWithAI_NoAI(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
	msg := WebSocketMessage{Type: "text-message", Data: map[string]interface{}{"content": "hi"}, Timestamp: time.Now()}
	// should return early with no panic
	c.processMessageWithAI(msg)
}

func TestProcessMessageWithAI_UnsupportedType(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	hub.SetAIService(&stubAI{reply: "ok"})
	c := &WebSocketClient{ID: "c", SessionID: "s", Hub: hub}
	// unsupported data type should be handled gracefully
	msg := WebSocketMessage{Type: "text-message", Data: 12345, Timestamp: time.Now()}
	c.processMessageWithAI(msg)
}
