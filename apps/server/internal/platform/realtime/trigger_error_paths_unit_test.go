package realtime

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"gorm.io/gorm"
)

// execTrigger 在测试库上建 SQLite trigger，注入写路径失败（message router
// 段仍使用；SLA 段已随 sla 模块迁移）。
func execTrigger(t *testing.T, db *gorm.DB, stmt string) {
	t.Helper()
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func TestWebRTC_HandleAnswerStateError(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	conn, err := s.CreatePeerConnection("sess-state")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	defer s.CloseConnection("sess-state") //nolint:errcheck

	// fresh connection is in stable state; applying an answer is an invalid transition
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	defer peer.Close() //nolint:errcheck
	svcOffer, err := conn.PeerConnection.CreateOffer(nil)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := peer.SetRemoteDescription(svcOffer); err == nil {
		// SDP may lack ice-ufrag; the goal is only to produce some answer payload
		answer, aerr := peer.CreateAnswer(nil)
		if aerr == nil {
			if err := s.HandleAnswer("sess-state", answer); err == nil {
				t.Fatal("expected invalid state transition error")
			}
			return
		}
	}
	// fallback: craft a syntactically valid answer that still violates the state machine
	if err := s.HandleAnswer("sess-state", webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n",
	}); err == nil {
		t.Fatal("expected SetRemoteDescription error on fresh connection")
	}
}

func TestWebRTC_HandleICECandidateWithoutRemote(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	if _, err := s.CreatePeerConnection("sess-ice"); err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	defer s.CloseConnection("sess-ice") //nolint:errcheck

	err := s.HandleICECandidate("sess-ice", webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 UDP 2130706431 192.168.1.1 5000 typ host",
	})
	if err == nil {
		t.Fatal("expected add candidate error without remote description")
	}
}

func TestWebRTC_SendDataChannelAfterClose(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	conn, err := s.CreatePeerConnection("sess-send")
	if err != nil {
		t.Fatalf("CreatePeerConnection: %v", err)
	}
	dc, err := conn.PeerConnection.CreateDataChannel("chan", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	conn.DataChannel = dc

	if err := conn.PeerConnection.Close(); err != nil {
		t.Fatalf("close pc: %v", err)
	}
	if err := s.SendDataChannelMessage("sess-send", "hello"); err == nil {
		t.Fatal("expected send error on closed channel")
	}
}

// ---- router misc ----

func TestMessageRouter_HandlePlatformMessages_AIError(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	r := NewMessageRouter(&failingRouterAI{}, hub, nil)

	adapter := &scriptableAdapter{msgChan: make(chan UnifiedMessage, 1)}
	r.RegisterPlatform(string(PlatformWeb), adapter)
	go r.handlePlatformMessages(string(PlatformWeb), adapter)

	adapter.msgChan <- UnifiedMessage{UserID: "u1", Content: "hi", Type: MessageTypeText, Timestamp: time.Now()}
	time.Sleep(100 * time.Millisecond)
	close(adapter.msgChan)
	time.Sleep(20 * time.Millisecond)
}
