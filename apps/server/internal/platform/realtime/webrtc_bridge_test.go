package realtime

// RA-7 SFU-lite 媒体桥接与 RA-5 信令归属路由的测试面：
// 前半是转发器/登记表/定向投递的纯单元分支（stub 读写面），后半是一条真实
// pion 回环桥接链路（访客发布视频轨 → 服务端转发 → 坐席经服务端重协商收轨）。

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestWebRTC_ForwarderTargetBookkeeping(t *testing.T) {
	fwd := &trackForwarder{sessionID: "sess-fw", publisherID: "pub", trackID: "t1"}
	if fwd.targetCount() != 0 || fwd.hasTarget("c1") {
		t.Fatal("empty forwarder should have no targets")
	}
	if key := forwarderKey(fwd); key != "pub/t1" {
		t.Fatalf("forwarderKey = %q, want pub/t1", key)
	}

	var firstWrites, replacedWrites, secondWrites int
	fwd.addTarget(forwardTarget{connID: "c1", write: func([]byte) (int, error) { firstWrites++; return 0, nil }})
	fwd.addTarget(forwardTarget{connID: "c2", write: func([]byte) (int, error) { secondWrites++; return 0, nil }})
	if !fwd.hasTarget("c1") || !fwd.hasTarget("c2") || fwd.targetCount() != 2 {
		t.Fatalf("bookkeeping after two addTarget calls: count=%d", fwd.targetCount())
	}

	// 同 connID 重复挂写面是替换而非追加（重协商换轨沿用原位置）
	fwd.addTarget(forwardTarget{connID: "c1", write: func([]byte) (int, error) { replacedWrites++; return 0, nil }})
	if fwd.targetCount() != 2 {
		t.Fatalf("duplicate connID should replace in place, got %d targets", fwd.targetCount())
	}
	fwd.drain([]byte{0x01})
	if firstWrites != 0 || replacedWrites != 1 || secondWrites != 1 {
		t.Fatalf("drain routing: first=%d replaced=%d second=%d", firstWrites, replacedWrites, secondWrites)
	}

	// 写面报错即撤下
	fwd.addTarget(forwardTarget{connID: "c3", write: func([]byte) (int, error) { return 0, errors.New("pc closed") }})
	fwd.drain([]byte{0x01})
	if fwd.hasTarget("c3") {
		t.Fatal("failing write side should be removed on drain")
	}

	fwd.removeTarget("c1")
	if fwd.hasTarget("c1") || fwd.targetCount() != 1 {
		t.Fatalf("removeTarget left %d targets", fwd.targetCount())
	}
}

func TestWebRTC_ForwarderRunForwardsAndExits(t *testing.T) {
	packet := []byte{0x80, 0x60, 0x01, 0x02, 0x03, 0x04}
	srcCalls := 0
	var writes int
	var lastPacket []byte
	fwd := &trackForwarder{
		src: func(buf []byte) (int, error) {
			srcCalls++
			switch srcCalls {
			case 1:
				copy(buf, packet)
				return len(packet), nil
			case 2:
				// 包有效但源已终止：先排空再退出
				copy(buf, packet)
				return len(packet), errors.New("source closed")
			default:
				return 0, io.EOF
			}
		},
	}
	fwd.addTarget(forwardTarget{connID: "c1", write: func(b []byte) (int, error) {
		writes++
		lastPacket = append([]byte(nil), b...)
		return 0, nil
	}})

	done := make(chan struct{})
	go func() {
		fwd.run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwarder loop did not exit on source error")
	}
	if writes != 2 {
		t.Fatalf("drained writes = %d, want 2", writes)
	}
	if !bytes.Equal(lastPacket, packet) {
		t.Fatalf("forwarded packet = %v, want %v", lastPacket, packet)
	}
}

func TestWebRTC_RegisterForwarderIdempotent(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	fwd := &trackForwarder{sessionID: "sess-reg", publisherID: "pub", trackID: "t1"}
	if !s.registerForwarder(fwd) {
		t.Fatal("first registration should succeed")
	}
	if s.registerForwarder(fwd) {
		t.Fatal("duplicate registration should be rejected")
	}
	if s.forwarderCount() != 1 {
		t.Fatalf("forwarderCount = %d, want 1", s.forwarderCount())
	}
}

func TestWebRTC_DropForwarderState(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	pub := &trackForwarder{sessionID: "sess-drop", publisherID: "c-p1", trackID: "t1"}
	sub := &trackForwarder{sessionID: "sess-drop", publisherID: "c-p2", trackID: "t2"}
	sub.addTarget(forwardTarget{connID: "c-p1", write: func([]byte) (int, error) { return 0, nil }})
	other := &trackForwarder{sessionID: "sess-other", publisherID: "c-p1", trackID: "t1-other"}
	for _, fwd := range []*trackForwarder{pub, sub, other} {
		if !s.registerForwarder(fwd) {
			t.Fatal("seed registration should succeed")
		}
	}

	s.mutex.Lock()
	s.dropForwarderStateLocked("sess-drop", "c-p1")
	s.mutex.Unlock()

	if s.forwarderCount() != 2 {
		t.Fatalf("forwarderCount = %d, want 2 (own entry dropped, other session kept)", s.forwarderCount())
	}
	if sub.hasTarget("c-p1") {
		t.Fatal("subscription write side of dropped connection should be removed")
	}
	if !s.registerForwarder(pub) {
		t.Fatal("publisher entry should have been deleted and re-registrable")
	}
}

func TestWebSocketHub_SendToClientBranches(t *testing.T) {
	hub := NewWebSocketHub()
	go hub.Run()

	// 未注册连接：静默返回
	hub.SendToClient("missing", WebSocketMessage{Type: "x"})

	full := &WebSocketClient{ID: "c-full", SessionID: "s", Send: make(chan WebSocketMessage, 1), Hub: hub}
	hub.register <- full
	ok := &WebSocketClient{ID: "c-ok", SessionID: "s", Send: make(chan WebSocketMessage, 4), Hub: hub}
	hub.register <- ok
	time.Sleep(20 * time.Millisecond)

	// 队列满：丢弃不阻塞
	full.Send <- WebSocketMessage{Type: "fill"}
	hub.SendToClient("c-full", WebSocketMessage{Type: "dropped"})

	hub.SendToClient("c-ok", WebSocketMessage{Type: "hit"})
	select {
	case msg := <-ok.Send:
		if msg.Type != "hit" {
			t.Fatalf("received frame %s, want hit", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("targeted send did not arrive")
	}
	select {
	case msg := <-full.Send:
		if msg.Type != "fill" {
			t.Fatalf("full queue frame = %s, want untouched fill", msg.Type)
		}
	default:
		t.Fatal("full queue should still hold the original message")
	}
}

func TestWebRTC_ConnByOwnerRouting(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	t.Cleanup(func() { _ = s.CloseConnection("sess-owner") })
	a, err := s.createPeerConnection("sess-owner", "client-a")
	if err != nil {
		t.Fatalf("peer a: %v", err)
	}
	b, err := s.createPeerConnection("sess-owner", "client-b")
	if err != nil {
		t.Fatalf("peer b: %v", err)
	}

	got, err := s.connByOwner("client-b", "sess-owner")
	if err != nil || got != b {
		t.Fatalf("owner routing: conn=%v err=%v, want client-b's connection", got, err)
	}

	// 归属未命中：按会话也找不到该归属的连接
	if err := s.HandleAnswer("ghost", "sess-owner", webrtc.SessionDescription{}); err == nil {
		t.Fatal("answer from unknown owner should miss")
	}
	if err := s.HandleICECandidate("ghost", "sess-owner", webrtc.ICECandidateInit{}); err == nil {
		t.Fatal("candidate from unknown owner should miss")
	}

	// 无归属回退：按会话首个命中（任一条，会话一致即可）
	fallback, err := s.connByOwner("", "sess-owner")
	if err != nil {
		t.Fatalf("legacy fallback: %v", err)
	}
	if fallback.SessionID != "sess-owner" || (fallback != a && fallback != b) {
		t.Fatalf("legacy fallback conn = %+v", fallback)
	}
	if _, err := s.connByOwner("", "sess-missing"); err == nil {
		t.Fatal("missing session should miss")
	}
}

func drainBridgeMessages(ch chan WebSocketMessage) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func assertNoBridgeFrame(t *testing.T, ch chan WebSocketMessage) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("unexpected frame %s", msg.Type)
	case <-time.After(150 * time.Millisecond):
	}
}

// waitForBridgeOffer 等待一条 webrtc-offer 帧并解析出其中的 offer（跳过同
// 通道上先到的候选/状态帧）。
func waitForBridgeOffer(t *testing.T, ch chan WebSocketMessage) webrtc.SessionDescription {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg.Type != "webrtc-offer" {
				continue
			}
			raw, _ := json.Marshal(msg.Data)
			var desc webrtc.SessionDescription
			if err := json.Unmarshal(raw, &desc); err != nil || desc.SDP == "" || desc.Type != webrtc.SDPTypeOffer {
				t.Fatalf("offer payload = %s err=%v", raw, err)
			}
			return desc
		case <-deadline:
			t.Fatalf("timed out waiting for webrtc-offer frame")
			return webrtc.SessionDescription{}
		}
	}
}

func TestWebRTC_NegotiateRenegotiationOffer(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	hub := s.wsHub
	ownerCh := make(chan WebSocketMessage, 256)
	hub.register <- &WebSocketClient{ID: "c-owner", SessionID: "sess-neg", Send: ownerCh, Hub: hub}
	broadcastCh := make(chan WebSocketMessage, 256)
	hub.register <- &WebSocketClient{ID: "c-broadcast", SessionID: "sess-neg-broadcast", Send: broadcastCh, Hub: hub}
	time.Sleep(20 * time.Millisecond)
	t.Cleanup(func() {
		_ = s.CloseConnection("sess-neg")
		_ = s.CloseConnection("sess-neg-broadcast")
	})

	conn, err := s.createPeerConnection("sess-neg", "c-owner")
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	if _, err := conn.PeerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("transceiver: %v", err)
	}

	// CreateOffer 失败：仅记日志，不外发（PC 保持 new 状态）
	hscovSetSeam(t, &hookPeerConnectionCreateOfferVar,
		func(*webrtc.PeerConnection, *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
			return webrtc.SessionDescription{}, errors.New("boom: offer")
		})
	drainBridgeMessages(ownerCh)
	conn.negotiate()
	assertNoBridgeFrame(t, ownerCh)

	// SetLocalDescription 失败：仅记日志，不外发（PC 仍保持 new 状态）
	hscovSetSeam(t, &hookPeerConnectionCreateOfferVar,
		func(pc *webrtc.PeerConnection, options *webrtc.OfferOptions) (webrtc.SessionDescription, error) {
			return pc.CreateOffer(options)
		})
	hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
		func(*webrtc.PeerConnection, webrtc.SessionDescription) error {
			return errors.New("boom: local")
		})
	conn.negotiate()
	assertNoBridgeFrame(t, ownerCh)

	// 成功路径：offer 定向投给归属连接
	hscovSetSeam(t, &hookPeerConnectionSetLocalDescriptionVar,
		func(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) error {
			return pc.SetLocalDescription(desc)
		})
	conn.negotiate()
	waitForBridgeOffer(t, ownerCh)

	// 等 ICE 收集完成，通道里的候选帧落地为稳态，后续广播断言不再被候选噪声干扰
	gatherDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(gatherDeadline) && conn.PeerConnection.ICEGatheringState() != webrtc.ICEGatheringStateComplete {
		time.Sleep(50 * time.Millisecond)
	}
	drainBridgeMessages(ownerCh)

	// 无归属回退：会话广播送达同会话客户端
	bare, err := s.CreatePeerConnection("sess-neg-broadcast")
	if err != nil {
		t.Fatalf("bare peer: %v", err)
	}
	if _, err := bare.PeerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("bare transceiver: %v", err)
	}
	bare.negotiate()
	waitForBridgeOffer(t, broadcastCh)
}

func TestWebRTC_AttachForwarderBranches(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	t.Cleanup(func() { _ = s.CloseConnection("sess-attach") })
	conn, err := s.CreatePeerConnection("sess-attach")
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	fwd := &trackForwarder{
		sessionID:   "sess-attach",
		publisherID: "other",
		trackID:     "t-attach",
		codec:       webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
	}

	// 本地轨构造失败分支
	hscovSetSeam(t, &hookNewTrackLocalStaticRTPVar,
		func(webrtc.RTPCodecCapability, string, string) (*webrtc.TrackLocalStaticRTP, error) {
			return nil, errors.New("boom: track")
		})
	s.attachForwarder(conn, fwd)
	if fwd.targetCount() != 0 {
		t.Fatal("constructor failure should not attach a write side")
	}
	hscovSetSeam(t, &hookNewTrackLocalStaticRTPVar,
		func(codec webrtc.RTPCodecCapability, id, streamID string) (*webrtc.TrackLocalStaticRTP, error) {
			return webrtc.NewTrackLocalStaticRTP(codec, id, streamID)
		})

	// AddTrack 失败分支：PC 已关
	closed, err := s.CreatePeerConnection("sess-attach")
	if err != nil {
		t.Fatalf("closed peer: %v", err)
	}
	if err := closed.PeerConnection.Close(); err != nil {
		t.Fatalf("close peer: %v", err)
	}
	s.attachForwarder(closed, fwd)
	if fwd.hasTarget(closed.ID) {
		t.Fatal("AddTrack on closed connection should not attach a write side")
	}

	// 成功 + hasTarget 幂等跳过
	s.attachForwarder(conn, fwd)
	if fwd.targetCount() != 1 || !fwd.hasTarget(conn.ID) {
		t.Fatalf("attach left %d targets", fwd.targetCount())
	}
	s.attachForwarder(conn, fwd)
	if fwd.targetCount() != 1 {
		t.Fatalf("duplicate attach should be skipped, got %d targets", fwd.targetCount())
	}
}

func TestWebRTC_FanOutTrackFiltering(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	t.Cleanup(func() {
		_ = s.CloseConnection("sess-fan")
		_ = s.CloseConnection("sess-fan-other")
	})
	publisher, err := s.CreatePeerConnection("sess-fan")
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	live, err := s.CreatePeerConnection("sess-fan")
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	terminal, err := s.CreatePeerConnection("sess-fan")
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	terminal.SetStatus("failed")
	if _, err := s.CreatePeerConnection("sess-fan-other"); err != nil {
		t.Fatalf("other session: %v", err)
	}

	fwd := &trackForwarder{
		sessionID:   "sess-fan",
		publisherID: publisher.ID,
		trackID:     "t-fan",
		codec:       webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
	}
	s.fanOutTrack(publisher, fwd)
	if fwd.targetCount() != 1 || !fwd.hasTarget(live.ID) {
		t.Fatalf("fan-out left %d targets, want only the live sibling", fwd.targetCount())
	}
	if fwd.hasTarget(terminal.ID) {
		t.Fatal("terminal connection must not be targeted")
	}
}

func TestWebRTC_ReplicateTracksTo(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	t.Cleanup(func() {
		_ = s.CloseConnection("sess-rep")
		_ = s.CloseConnection("sess-rep-other")
	})
	existing, err := s.CreatePeerConnection("sess-rep")
	if err != nil {
		t.Fatalf("existing: %v", err)
	}
	fwd := &trackForwarder{
		sessionID:   "sess-rep",
		publisherID: existing.ID,
		trackID:     "t-rep",
		codec:       webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
	}
	if !s.registerForwarder(fwd) {
		t.Fatal("seed registration should succeed")
	}

	// 晚加入者补投
	late, err := s.CreatePeerConnection("sess-rep")
	if err != nil {
		t.Fatalf("late: %v", err)
	}
	s.replicateTracksTo(late)
	if fwd.targetCount() != 1 || !fwd.hasTarget(late.ID) {
		t.Fatalf("replication left %d targets", fwd.targetCount())
	}

	// 发布方自身与其它会话不投
	s.replicateTracksTo(existing)
	stranger, err := s.CreatePeerConnection("sess-rep-other")
	if err != nil {
		t.Fatalf("stranger: %v", err)
	}
	s.replicateTracksTo(stranger)
	if fwd.targetCount() != 1 || fwd.hasTarget(stranger.ID) {
		t.Fatalf("replication should stay at the late joiner only, got %d targets", fwd.targetCount())
	}
}

func TestWebRTC_PublishForwarderDuplicate(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	t.Cleanup(func() { _ = s.CloseConnection("sess-pub") })
	publisher, err := s.CreatePeerConnection("sess-pub")
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	sibling, err := s.CreatePeerConnection("sess-pub")
	if err != nil {
		t.Fatalf("sibling: %v", err)
	}
	fwd := &trackForwarder{
		sessionID:   "sess-pub",
		publisherID: publisher.ID,
		trackID:     "t-pub",
		codec:       webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		src:         func([]byte) (int, error) { return 0, io.EOF },
	}

	publisher.publishForwarder(fwd)
	publisher.publishForwarder(fwd) // 幂等跳过

	if s.forwarderCount() != 1 {
		t.Fatalf("forwarderCount = %d, want 1", s.forwarderCount())
	}
	if !fwd.hasTarget(sibling.ID) || fwd.targetCount() != 1 {
		t.Fatalf("publish left %d targets, want the live sibling once", fwd.targetCount())
	}
}

// vp8PayloadTypeFromSDP 从 SDP 里解析 VP8 的动态 payload type（手写 RTP 包
// 需要按协商结果填 PT，OnTrack 才能匹配到轨）。
func vp8PayloadTypeFromSDP(t *testing.T, sdp string) uint8 {
	t.Helper()
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "a=rtpmap:") {
			continue
		}
		fields := strings.SplitN(strings.TrimPrefix(line, "a=rtpmap:"), " ", 2)
		if len(fields) == 2 && strings.HasPrefix(fields[1], "VP8/") {
			pt, err := strconv.Atoi(fields[0])
			if err != nil {
				t.Fatalf("bad rtpmap %q: %v", line, err)
			}
			return uint8(pt)
		}
	}
	t.Fatalf("no VP8 rtpmap in SDP:\n%s", sdp)
	return 0
}

// ssrcFromSDP 取首个 a=ssrc 行的 SSRC（访客端发布轨在 SDP 里声明的发送 SSRC）。
func ssrcFromSDP(t *testing.T, sdp string) uint32 {
	t.Helper()
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "a=ssrc:") {
			fields := strings.SplitN(strings.TrimPrefix(line, "a=ssrc:"), " ", 2)
			v, err := strconv.ParseUint(fields[0], 10, 32)
			if err != nil {
				t.Fatalf("bad ssrc %q: %v", line, err)
			}
			return uint32(v)
		}
	}
	t.Fatalf("no a=ssrc line in SDP:\n%s", sdp)
	return 0
}

// rtpPacket 手写最小 RTP 包（12 字节头 + 4 字节载荷），足以驱动 pion OnTrack。
func rtpPacket(pt uint8, seq uint16, timestamp, ssrc uint32) []byte {
	packet := make([]byte, 16)
	packet[0] = 0x80
	packet[1] = pt & 0x7f
	binary.BigEndian.PutUint16(packet[2:], seq)
	binary.BigEndian.PutUint32(packet[4:], timestamp)
	binary.BigEndian.PutUint32(packet[8:], ssrc)
	copy(packet[12:], []byte{0xde, 0xad, 0xbe, 0xef})
	return packet
}

func waitBridgeConnected(t *testing.T, pc *webrtc.PeerConnection) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("peer connection state = %s, want connected", pc.ConnectionState())
}

// TestWebRTC_LoopbackMediaBridge 端到端验证 RA-7/RA-5：访客在首 offer 里发布
// 视频轨，服务端 PC OnTrack 后建转发轨挂到坐席 PC 并发起重协商，坐席在同一条
// PC 上应答后收到桥接轨。候选/应答全走归属定向路由，路由错则桥接不成立。
func TestWebRTC_LoopbackMediaBridge(t *testing.T) {
	s, _ := newUnitWebRTCService(t)
	hub := s.wsHub
	const sess = "sess-bridge"

	visitorCh := make(chan WebSocketMessage, 64)
	agentCh := make(chan WebSocketMessage, 64)
	hub.register <- &WebSocketClient{ID: "c-visitor", SessionID: sess, Send: visitorCh, Hub: hub}
	hub.register <- &WebSocketClient{ID: "c-agent", SessionID: sess, Send: agentCh, Hub: hub}
	time.Sleep(20 * time.Millisecond)
	t.Cleanup(func() { _ = s.CloseConnection(sess) })

	newPC := func() *webrtc.PeerConnection {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatalf("peer: %v", err)
		}
		t.Cleanup(func() { _ = pc.Close() })
		return pc
	}
	visitorPC := newPC()
	agentPC := newPC()

	// 访客端发布视频轨（SDK 形态：轨先加，offer 里带 sendonly m-line）
	visitorTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video-visitor", "screen")
	if err != nil {
		t.Fatalf("visitor track: %v", err)
	}
	if _, err := visitorPC.AddTrack(visitorTrack); err != nil {
		t.Fatalf("visitor AddTrack: %v", err)
	}
	offerVisitor, err := visitorPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("visitor offer: %v", err)
	}
	if err := visitorPC.SetLocalDescription(offerVisitor); err != nil {
		t.Fatalf("visitor local: %v", err)
	}
	answerVisitor, err := s.HandleOffer("c-visitor", sess, *visitorPC.LocalDescription())
	if err != nil {
		t.Fatalf("visitor HandleOffer: %v", err)
	}
	if err := visitorPC.SetRemoteDescription(*answerVisitor); err != nil {
		t.Fatalf("visitor remote: %v", err)
	}

	// 坐席端 recvonly（admin 形态：offerToReceiveVideo）
	if _, err := agentPC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("agent transceiver: %v", err)
	}
	offerAgent, err := agentPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("agent offer: %v", err)
	}
	if err := agentPC.SetLocalDescription(offerAgent); err != nil {
		t.Fatalf("agent local: %v", err)
	}
	answerAgent, err := s.HandleOffer("c-agent", sess, *agentPC.LocalDescription())
	if err != nil {
		t.Fatalf("agent HandleOffer: %v", err)
	}
	if err := agentPC.SetRemoteDescription(*answerAgent); err != nil {
		t.Fatalf("agent remote: %v", err)
	}

	// 客户端候选按归属注入服务端对应 PC
	visitorPC.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		_ = s.HandleICECandidate("c-visitor", sess, c.ToJSON())
	})
	agentPC.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		_ = s.HandleICECandidate("c-agent", sess, c.ToJSON())
	})

	// 服务端信令泵：候选回注测试端 PC；坐席端另处理服务端重协商 offer
	pump := func(ch chan WebSocketMessage, pc *webrtc.PeerConnection, owner string, handleOffer bool) {
		go func() {
			for msg := range ch {
				if msg.Type == "webrtc-candidate" {
					data, _ := msg.Data.(map[string]interface{})
					raw, _ := data["candidate"].(json.RawMessage)
					var init webrtc.ICECandidateInit
					if json.Unmarshal(raw, &init) == nil {
						_ = pc.AddICECandidate(init)
					}
					continue
				}
				if !handleOffer || msg.Type != "webrtc-offer" {
					continue
				}
				raw, _ := json.Marshal(msg.Data)
				var desc webrtc.SessionDescription
				if json.Unmarshal(raw, &desc) != nil {
					continue
				}
				if pc.SignalingState() != webrtc.SignalingStateStable || pc.SetRemoteDescription(desc) != nil {
					continue
				}
				answer, err := pc.CreateAnswer(nil)
				if err != nil || pc.SetLocalDescription(answer) != nil {
					continue
				}
				_ = s.HandleAnswer(owner, sess, *pc.LocalDescription())
			}
		}()
	}
	pump(visitorCh, visitorPC, "c-visitor", false)
	pump(agentCh, agentPC, "c-agent", true)

	waitBridgeConnected(t, visitorPC)
	waitBridgeConnected(t, agentPC)

	agentGotTrack := make(chan *webrtc.TrackRemote, 4)
	agentPC.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		agentGotTrack <- track
	})

	// 访客持续发 RTP，直到坐席端收到桥接轨
	pt := vp8PayloadTypeFromSDP(t, visitorPC.LocalDescription().SDP)
	ssrc := ssrcFromSDP(t, visitorPC.LocalDescription().SDP)
	stopWrite := make(chan struct{})
	go func() {
		var seq uint16
		for {
			select {
			case <-stopWrite:
				return
			default:
			}
			if _, err := visitorTrack.Write(rtpPacket(pt, seq, uint32(seq)*3000, ssrc)); err != nil {
				return
			}
			seq++
			time.Sleep(50 * time.Millisecond)
		}
	}()

	select {
	case track := <-agentGotTrack:
		close(stopWrite)
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			t.Fatalf("bridged track kind = %s, want video", track.Kind())
		}
		if track.Codec().MimeType != webrtc.MimeTypeVP8 {
			t.Fatalf("bridged codec = %s, want VP8", track.Codec().MimeType)
		}
	case <-time.After(20 * time.Second):
		close(stopWrite)
		t.Fatal("agent peer did not receive the bridged track within timeout")
	}
}
