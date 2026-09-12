package services

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

type unitVoiceLifecycle struct {
	started  []string
	answered []string
	ended    []string
}

func (v *unitVoiceLifecycle) StartCall(ctx context.Context, sessionID, connectionID string) {
	v.started = append(v.started, connectionID)
}

func (v *unitVoiceLifecycle) AnswerCall(ctx context.Context, connectionID string) {
	v.answered = append(v.answered, connectionID)
}

func (v *unitVoiceLifecycle) EndCall(ctx context.Context, connectionID string) {
	v.ended = append(v.ended, connectionID)
}

func newUnitWebRTCService(t *testing.T) (*WebRTCService, *unitVoiceLifecycle) {
	t.Helper()
	hub := NewWebSocketHub()
	go hub.Run()
	s := NewWebRTCService("stun:127.0.0.1:13478", hub)
	voice := &unitVoiceLifecycle{}
	s.SetVoiceLifecycle(voice)
	return s, voice
}

func TestWebRTC_CreatePeerConnection_InvalidStun(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	s := NewWebRTCService("", hub)
	if _, err := s.CreatePeerConnection("sess"); err == nil {
		t.Fatal("expected error for empty STUN server")
	}
}

func TestWebRTC_FullSignalingFlow(t *testing.T) {
	s, voice := newUnitWebRTCService(t)

	conn, err := s.CreatePeerConnection("sess-full")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	if s.GetConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", s.GetConnectionCount())
	}
	if len(voice.started) != 1 {
		t.Fatalf("expected StartCall, got %+v", voice.started)
	}

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	defer peer.Close() //nolint:errcheck
	if _, err := peer.CreateDataChannel("data", nil); err != nil {
		t.Fatalf("data channel: %v", err)
	}

	// Service connection must be in have-local-offer state to accept an answer:
	// add a data channel so the offer contains a media section, create the offer
	// locally, then let the test peer answer it.
	if _, err := conn.PeerConnection.CreateDataChannel("svc", nil); err != nil {
		t.Fatalf("service data channel: %v", err)
	}
	svcOffer, err := conn.PeerConnection.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := conn.PeerConnection.SetLocalDescription(svcOffer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	enrichedOffer := conn.PeerConnection.LocalDescription()
	if err := peer.SetRemoteDescription(*enrichedOffer); err != nil {
		t.Fatalf("peer SetRemoteDescription: %v", err)
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("CreateAnswer: %v", err)
	}

	if err := s.HandleAnswer("sess-full", answer); err != nil {
		t.Fatalf("HandleAnswer: %v", err)
	}
	if len(voice.answered) != 1 {
		t.Fatalf("expected AnswerCall, got %+v", voice.answered)
	}

	if err := s.HandleICECandidate("sess-full", webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 UDP 2130706431 192.168.1.1 5000 typ host",
	}); err != nil {
		t.Fatalf("HandleICECandidate: %v", err)
	}

	stats, err := s.GetConnectionStats("sess-full")
	if err != nil {
		t.Fatalf("GetConnectionStats: %v", err)
	}
	if stats["session_id"] != "sess-full" {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if err := s.SendDataChannelMessage("sess-full", "hello"); err == nil {
		t.Fatal("expected error without data channel")
	}

	if err := s.CloseConnection("sess-full"); err != nil {
		t.Fatalf("CloseConnection: %v", err)
	}
	if s.GetConnectionCount() != 0 {
		t.Fatalf("expected 0 connections, got %d", s.GetConnectionCount())
	}
}

func TestWebRTC_HandleOfferGeneratesAnswer(t *testing.T) {
	s, _ := newUnitWebRTCService(t)

	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	defer offerer.Close() //nolint:errcheck
	if _, err := offerer.CreateDataChannel("d", nil); err != nil {
		t.Fatalf("data channel: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}

	answer, err := s.HandleOffer("sess-offer", offer)
	if err != nil {
		t.Fatalf("HandleOffer: %v", err)
	}
	if answer.Type != webrtc.SDPTypeAnswer {
		t.Fatalf("unexpected answer type: %s", answer.Type)
	}

	// invalid remote description triggers the SetRemoteDescription error path
	if _, err := s.HandleOffer("sess-bad", webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "junk"}); err == nil {
		t.Fatal("expected SetRemoteDescription error")
	}

	if err := s.CloseConnection("sess-offer"); err != nil {
		t.Fatalf("CloseConnection: %v", err)
	}
}

func TestWebRTC_HandleAnswerAndCandidateMissingConn(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	if err := s.HandleAnswer("missing", webrtc.SessionDescription{}); err == nil {
		t.Fatal("expected missing connection error")
	}
	if err := s.HandleICECandidate("missing", webrtc.ICECandidateInit{}); err == nil {
		t.Fatal("expected missing connection error")
	}
	if _, err := s.GetConnectionStats("missing"); err == nil {
		t.Fatal("expected missing connection error")
	}
	if err := s.SendDataChannelMessage("missing", "x"); err == nil {
		t.Fatal("expected missing connection error")
	}
	if err := s.CloseConnection("missing"); err != nil {
		t.Fatalf("CloseConnection on empty map should succeed: %v", err)
	}
}

func TestWebRTC_DataChannelStats(t *testing.T) {
	s, _ := newUnitWebRTCService(t)

	conn, err := s.CreatePeerConnection("sess-dc")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}

	// attach a data channel via the exported field to hit stats branch
	dc, err := conn.PeerConnection.CreateDataChannel("chan", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	conn.DataChannel = dc

	stats, err := s.GetConnectionStats("sess-dc")
	if err != nil {
		t.Fatalf("GetConnectionStats: %v", err)
	}
	dcStats, ok := stats["data_channel"].(map[string]interface{})
	if !ok || dcStats["label"] != "chan" {
		t.Fatalf("unexpected data channel stats: %+v", stats["data_channel"])
	}

	if err := s.CloseConnection("sess-dc"); err != nil {
		t.Fatalf("CloseConnection: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}
