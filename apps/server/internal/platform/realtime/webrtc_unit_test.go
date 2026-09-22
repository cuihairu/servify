package realtime

import (
	"context"
	"servify/apps/server/internal/platform/iceturn"
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
	s := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:127.0.0.1:13478"}}, hub)
	voice := &unitVoiceLifecycle{}
	s.SetVoiceLifecycle(voice)
	return s, voice
}

func TestWebRTC_iceServersMapping(t *testing.T) {
	stunOnly := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:a:3478"}}, NewWebSocketHub())
	got := stunOnly.iceServers()
	if len(got) != 1 || got[0].URLs[0] != "stun:a:3478" || got[0].Username != "" {
		t.Fatalf("stun-only mapping = %+v, want single credential-free STUN", got)
	}

	withTurn := NewWebRTCService(iceturn.ICEConfig{
		STUNServers:    []string{"stun:a:3478"},
		TURNURL:        "turn:turn.example.com:3478",
		TURNUsername:   "1790000300",
		TURNCredential: "isReWBKNlmmMSS3VR4Xr9PtPqYE=",
	}, NewWebSocketHub())
	got = withTurn.iceServers()
	if len(got) != 2 {
		t.Fatalf("stun+turn mapping length = %d, want 2", len(got))
	}
	turn := got[1]
	if turn.URLs[0] != "turn:turn.example.com:3478" || turn.Username != "1790000300" || turn.Credential != "isReWBKNlmmMSS3VR4Xr9PtPqYE=" {
		t.Fatalf("TURN ICEServer = %+v, want url/username/credential carried over", turn)
	}
}

func TestWebRTC_ICEConfigPayload(t *testing.T) {
	stunOnly := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:a:3478"}}, NewWebSocketHub())
	payload, ok := stunOnly.ICEConfigPayload()
	if !ok {
		t.Fatal("stun-only payload should be deliverable")
	}
	entries := payload["ice_servers"].([]map[string]interface{})
	if len(entries) != 1 {
		t.Fatalf("stun-only entries = %+v, want single STUN", entries)
	}
	if _, hasUser := entries[0]["username"]; hasUser {
		t.Fatalf("STUN entry must not carry credentials: %+v", entries[0])
	}
	if entries[0]["urls"].([]string)[0] != "stun:a:3478" {
		t.Fatalf("stun url = %+v, want stun:a:3478", entries[0])
	}

	withTurn := NewWebRTCService(iceturn.ICEConfig{
		STUNServers:    []string{"stun:a:3478"},
		TURNURL:        "turn:turn.example.com:3478",
		TURNUsername:   "1790000300",
		TURNCredential: "isReWBKNlmmMSS3VR4Xr9PtPqYE=",
		TURNTTL:        5 * time.Minute,
	}, NewWebSocketHub())
	payload, ok = withTurn.ICEConfigPayload()
	if !ok {
		t.Fatal("turn payload should be deliverable")
	}
	entries = payload["ice_servers"].([]map[string]interface{})
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want stun+turn", len(entries))
	}
	turn := entries[1]
	if turn["username"] != "1790000300" || turn["credential"] != "isReWBKNlmmMSS3VR4Xr9PtPqYE=" || turn["ttl"] != int64(300) {
		t.Fatalf("turn entry = %+v, want username/credential/ttl carried over", turn)
	}

	empty := NewWebRTCService(iceturn.ICEConfig{}, NewWebSocketHub())
	if payload, ok := empty.ICEConfigPayload(); ok || payload != nil {
		t.Fatalf("empty ICE config = (%+v, %v), want (nil, false)", payload, ok)
	}
}

func TestWebRTC_HandleOfferCreatePeerConnectionError(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	// 非法 ICE URL 使 CreatePeerConnection 失败，HandleOffer 应原样上抛。
	s := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"://not-a-valid-ice-url"}}, hub)
	if _, err := s.HandleOffer("sess", webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0"}); err == nil {
		t.Fatal("expected HandleOffer to surface CreatePeerConnection error")
	}
}

func TestWebRTC_CreatePeerConnection_InvalidStun(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	// 空 ICE 配置合法（退化为 host 候选直连）；真正非法的是解析不了的 URL。
	s := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"://not-a-valid-ice-url"}}, hub)
	if _, err := s.CreatePeerConnection("sess"); err == nil {
		t.Fatal("expected error for malformed ICE URL")
	}
	// 空 ICE 配置（无 STUN/TURN）应能建连，走 host 候选。
	empty := NewWebRTCService(iceturn.ICEConfig{}, hub)
	if conn, err := empty.CreatePeerConnection("sess-empty-ice"); err != nil {
		t.Fatalf("empty ICE config should create connection: %v", err)
	} else {
		_ = conn.PeerConnection.Close()
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
