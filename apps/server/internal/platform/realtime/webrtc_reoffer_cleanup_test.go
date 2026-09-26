package realtime

import (
	"errors"
	"testing"

	"github.com/pion/webrtc/v4"

	"servify/apps/server/internal/platform/iceturn"
)

// buildTestOffer 用真实 offerer 生成合法 offer（与 seam 覆盖测试同款）。
func buildTestOffer(t *testing.T) webrtc.SessionDescription {
	t.Helper()
	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	t.Cleanup(func() { _ = offerer.Close() }) //nolint:errcheck
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
	return offer
}

// sessionConnIDs 列出某会话当前持有的连接 ID（测试内直接读 map，加锁口径一致）。
func sessionConnIDs(s *WebRTCService, sessionID string) map[string]bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	ids := map[string]bool{}
	for _, conn := range s.connections {
		if conn.SessionID == sessionID {
			ids[conn.ID] = true
		}
	}
	return ids
}

// TestWebRTC_HandleOfferReclaimsTerminalConnections RA-8 服务端半边：
// 重发 offer 前回收同 session 终态旧 PC；存活 PC 不受影响。
func TestWebRTC_HandleOfferReclaimsTerminalConnections(t *testing.T) {
	s, voice := newUnitWebRTCService(t)
	offer := buildTestOffer(t)

	live, err := s.CreatePeerConnection("sess-reoffer")
	if err != nil {
		t.Fatalf("CreatePeerConnection(live): %v", err)
	}
	stale, err := s.CreatePeerConnection("sess-reoffer")
	if err != nil {
		t.Fatalf("CreatePeerConnection(stale): %v", err)
	}
	stale.SetStatus("failed")

	if _, err := s.HandleOffer("sess-reoffer", offer); err != nil {
		t.Fatalf("HandleOffer: %v", err)
	}

	remaining := sessionConnIDs(s, "sess-reoffer")
	if len(remaining) != 2 {
		t.Fatalf("expected 2 connections (live + new), got %d: %v", len(remaining), remaining)
	}
	if remaining[live.ID] != true {
		t.Fatalf("live connection must survive re-offer: %v", remaining)
	}
	if remaining[stale.ID] {
		t.Fatalf("terminal connection must be reclaimed: %v", remaining)
	}
	found := false
	for _, id := range voice.ended {
		if id == stale.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("voice EndCall must fire for stale connection, ended=%v", voice.ended)
	}

	if err := s.CloseConnection("sess-reoffer"); err != nil {
		t.Fatalf("CloseConnection: %v", err)
	}
}

// TestWebRTC_CloseTerminalConnections_Branches 终态回收的分支面：
// voice 为 nil、close 报错仅记日志、其它会话连接不受波及。
func TestWebRTC_CloseTerminalConnections_Branches(t *testing.T) {
	t.Run("nil voice lifecycle skips EndCall", func(t *testing.T) {
		hub := NewWebSocketHub()
		go hub.Run()
		bare := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:127.0.0.1:13478"}}, hub)
		stale, err := bare.CreatePeerConnection("sess-bare")
		if err != nil {
			t.Fatalf("CreatePeerConnection: %v", err)
		}
		stale.SetStatus("closed")
		bare.closeTerminalConnections("sess-bare")
		if got := sessionConnIDs(bare, "sess-bare"); len(got) != 0 {
			t.Fatalf("terminal connection must be reclaimed without voice, got %v", got)
		}
	})

	t.Run("close error is logged and connection still removed", func(t *testing.T) {
		s, _ := newUnitWebRTCService(t)
		stale, err := s.CreatePeerConnection("sess-close-err")
		if err != nil {
			t.Fatalf("CreatePeerConnection: %v", err)
		}
		stale.SetStatus("disconnected")
		hscovSetSeam(t, &hookPeerConnectionClose, func(*webrtc.PeerConnection) error {
			return errors.New("boom: close")
		})
		s.closeTerminalConnections("sess-close-err")
		if got := sessionConnIDs(s, "sess-close-err"); len(got) != 0 {
			t.Fatalf("connection must still be removed after close error, got %v", got)
		}
	})

	t.Run("other sessions untouched", func(t *testing.T) {
		s, _ := newUnitWebRTCService(t)
		if _, err := s.CreatePeerConnection("sess-a"); err != nil {
			t.Fatalf("CreatePeerConnection(a): %v", err)
		}
		other, err := s.CreatePeerConnection("sess-b")
		if err != nil {
			t.Fatalf("CreatePeerConnection(b): %v", err)
		}
		other.SetStatus("failed")
		s.closeTerminalConnections("sess-a")
		if got := sessionConnIDs(s, "sess-b"); len(got) != 1 {
			t.Fatalf("other session connection must be untouched, got %v", got)
		}
	})
}
