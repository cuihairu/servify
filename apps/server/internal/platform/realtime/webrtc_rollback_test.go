package realtime

// Codex 深审 Major（2026-11-13）回归：HandleOffer 协商早期失败必须回滚连接。
// createPeerConnection 已把 PC 登记进连接表并启动语音生命周期；此前
// SetRemote/CreateAnswer/SetLocal 任一步失败直接 return，连接以 "created"
// 残留（非终态，终态回收集合收不到）→ 反复畸形 SDP 累积 PC 与语音呼叫。

import (
	"errors"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"

	"servify/apps/server/internal/platform/iceturn"
)

// validTestOffer 走真实 pion 生成一份带数据通道的合法 offer。
func validTestOffer(t *testing.T) webrtc.SessionDescription {
	t.Helper()
	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("offerer: %v", err)
	}
	t.Cleanup(func() { _ = offerer.Close() })
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

func assertRolledBack(t *testing.T, s *WebRTCService, voice *unitVoiceLifecycle, sessionID string, attempts int) {
	t.Helper()
	s.mutex.Lock()
	remaining := 0
	for _, conn := range s.connections {
		if conn.SessionID == sessionID {
			remaining++
		}
	}
	s.mutex.Unlock()
	if remaining != 0 {
		t.Fatalf("session %s still holds %d connections after failed negotiation", sessionID, remaining)
	}
	if len(voice.started) != len(voice.ended) {
		t.Fatalf("voice lifecycle leak: started=%v ended=%v", voice.started, voice.ended)
	}
	if attempts > 0 && len(voice.ended) != attempts {
		t.Fatalf("ended=%v want %d rollbacks", voice.ended, attempts)
	}
}

func TestHandleOfferRollsBackOnCreateAnswerError(t *testing.T) {
	s, voice := newUnitWebRTCService(t)
	offer := validTestOffer(t)

	hscovSetSeam(t, &hookPeerConnectionCreateAnswerVar,
		func(*webrtc.PeerConnection, *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
			return webrtc.SessionDescription{}, errors.New("boom: answer")
		})
	for i := 0; i < 3; i++ {
		if _, err := s.HandleOffer("", "sess-rollback-answer", offer); err == nil || !strings.Contains(err.Error(), "failed to create answer") {
			t.Fatalf("attempt %d: expected create answer error, got %v", i, err)
		}
	}
	assertRolledBack(t, s, voice, "sess-rollback-answer", 3)
}

func TestHandleOfferRollsBackOnSetLocalError(t *testing.T) {
	s, voice := newUnitWebRTCService(t)
	offer := validTestOffer(t)

	hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
		func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: set local")
		})
	if _, err := s.HandleOffer("", "sess-rollback-local", offer); err == nil || !strings.Contains(err.Error(), "failed to set local description") {
		t.Fatalf("expected set local description error, got %v", err)
	}
	assertRolledBack(t, s, voice, "sess-rollback-local", 1)
}

// 畸形 SDP：SetRemoteDescription 真实失败（无 seam，走 pion 校验路径）。
func TestHandleOfferRollsBackOnSetRemoteError(t *testing.T) {
	s, voice := newUnitWebRTCService(t)

	bad := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "v=0\r\nm=not-a-media-section\r\n"}
	if _, err := s.HandleOffer("", "sess-rollback-remote", bad); err == nil || !strings.Contains(err.Error(), "failed to set remote description") {
		t.Fatalf("expected set remote description error, got %v", err)
	}
	assertRolledBack(t, s, voice, "sess-rollback-remote", 1)
}

// PC Close 本身报错：回滚仍须完成（连接表移除、语音 EndCall），错误只记日志。
func TestHandleOfferRollbackSurvivesCloseError(t *testing.T) {
	s, voice := newUnitWebRTCService(t)
	offer := validTestOffer(t)

	hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
		func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: set local")
		})
	hscovSetSeam(t, &hookPeerConnectionCloseVar,
		func(*webrtc.PeerConnection) error { return errors.New("boom: close") })

	if _, err := s.HandleOffer("", "sess-rollback-closeerr", offer); err == nil {
		t.Fatal("expected negotiation error")
	}
	s.mutex.Lock()
	remaining := 0
	for _, conn := range s.connections {
		if conn.SessionID == "sess-rollback-closeerr" {
			remaining++
		}
	}
	s.mutex.Unlock()
	if remaining != 0 {
		t.Fatalf("close-error must not block rollback: %d connections remain", remaining)
	}
	if len(voice.ended) != 1 {
		t.Fatalf("ended=%v want 1", voice.ended)
	}
}

// 未装配语音生命周期的服务：回滚在 voice=nil 下照常工作。
func TestHandleOfferRollbackWithoutVoiceLifecycle(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()
	s := NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:127.0.0.1:13478"}}, hub)

	offer := validTestOffer(t)
	hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
		func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: set local")
		})
	if _, err := s.HandleOffer("", "sess-rollback-novoice", offer); err == nil {
		t.Fatal("expected negotiation error")
	}
	s.mutex.Lock()
	remaining := 0
	for _, conn := range s.connections {
		if conn.SessionID == "sess-rollback-novoice" {
			remaining++
		}
	}
	s.mutex.Unlock()
	if remaining != 0 {
		t.Fatalf("voice-less service must still roll back: %d remain", remaining)
	}
}

// 回滚后的重试可正常建连（占位泄漏不影响后续合法协商）。
func TestHandleOfferAfterRollbackStillWorks(t *testing.T) {
	s, voice := newUnitWebRTCService(t)
	offer := validTestOffer(t)

	// 先存真实实现，先注入失败再手动还原，第二次走真实 CreateAnswer
	hookMu.Lock()
	realCreateAnswer := hookPeerConnectionCreateAnswerVar
	hookMu.Unlock()
	hscovSetSeam(t, &hookPeerConnectionCreateAnswerVar,
		func(*webrtc.PeerConnection, *webrtc.AnswerOptions) (webrtc.SessionDescription, error) {
			return webrtc.SessionDescription{}, errors.New("boom: answer")
		})
	if _, err := s.HandleOffer("", "sess-rollback-retry", offer); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	hookMu.Lock()
	hookPeerConnectionCreateAnswerVar = realCreateAnswer
	hookMu.Unlock()

	answer, err := s.HandleOffer("", "sess-rollback-retry", offer)
	if err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
	if answer.Type != webrtc.SDPTypeAnswer {
		t.Fatalf("answer type=%v", answer.Type)
	}
	s.mutex.Lock()
	remaining := 0
	for _, conn := range s.connections {
		if conn.SessionID == "sess-rollback-retry" {
			remaining++
		}
	}
	s.mutex.Unlock()
	if remaining != 1 {
		t.Fatalf("connections=%d want exactly the live one", remaining)
	}
	// 成功那次 StartCall 不回滚：ended 恰 1 次（首次失败）
	if len(voice.started) != 2 || len(voice.ended) != 1 {
		t.Fatalf("voice started=%v ended=%v", voice.started, voice.ended)
	}
}
