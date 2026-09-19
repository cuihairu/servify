package realtime

import (
	"github.com/pion/webrtc/v4"
	"testing"
)

func TestWebRTCService_NoConnectionErrors(t *testing.T) {
	s := NewWebRTCService("stun:stun.l.google.com:19302", NewWebSocketHub())

	if _, err := s.GetConnectionStats("nope"); err == nil {
		t.Fatalf("expected error for missing connection stats")
	}
	if err := s.HandleAnswer("nope", webrtc.SessionDescription{}); err == nil {
		t.Fatalf("expected error for missing connection answer")
	}
	if err := s.HandleICECandidate("nope", webrtc.ICECandidateInit{}); err == nil {
		t.Fatalf("expected error for missing connection candidate")
	}
	if err := s.SendDataChannelMessage("nope", "hi"); err == nil {
		t.Fatalf("expected error for missing data channel")
	}
}

// no helpers
