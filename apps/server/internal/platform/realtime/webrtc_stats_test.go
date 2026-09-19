package realtime

import (
	"testing"
)

func TestWebRTCService_GetStats_UnknownSession(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	s := NewWebRTCService("stun:stun.l.google.com:19302", hub)
	if _, err := s.GetConnectionStats("no-such-session"); err == nil {
		t.Fatalf("expected error for unknown session id")
	}
}
