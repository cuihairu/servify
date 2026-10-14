package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"

	"servify/apps/server/internal/platform/iceturn"
)

type VoiceLifecycle interface {
	StartCall(ctx context.Context, sessionID string, connectionID string)
	AnswerCall(ctx context.Context, connectionID string)
	EndCall(ctx context.Context, connectionID string)
}

type WebRTCService struct {
	api         *webrtc.API
	connections map[string]*WebRTCConnection
	mutex       sync.RWMutex
	ice         iceturn.ICEConfig
	wsHub       *WebSocketHub
	voice       VoiceLifecycle
	// forwards 是同会话媒体桥接的转发注册表（RA-7 SFU-lite）：
	// key = 发布连接 ID / 轨道 ID，一个发布轨一条读面 + N 条订阅端写面。
	forwards map[string]*trackForwarder
}

type WebRTCConnection struct {
	ID             string
	SessionID      string
	PeerConnection *webrtc.PeerConnection
	DataChannel    *webrtc.DataChannel
	Status         string
	CreatedAt      time.Time
	// ownerClientID 是本连接的信令归属 WS 连接：同一会话可能有访客/坐席两条
	// PC，信令必须按归属定向投递，按会话首个命中会把应答/候选投给无关对端。
	// 空值表示无归属（REST 面等旧调用方），回退按会话首个命中。
	ownerClientID string
	// svc 供 pion 回调闭包回到服务层（转发登记、重协商下发）。
	svc *WebRTCService

	statusMu      sync.RWMutex
	dcMu          sync.RWMutex
	negotiationMu sync.Mutex
}

// rtpPacketBufferSize 单包缓冲：RTP 载荷上限 65535 字节，留一字节余量。
const rtpPacketBufferSize = 65536

// rtpReader 读出一个原始 RTP 包；TrackRemote 的读面不并发安全，
// 每条发布轨只由一个转发 goroutine 读。
type rtpReader func(buf []byte) (int, error)

// rtpWriter 写出一个原始 RTP 包；TrackLocalStaticRTP 的写面。
type rtpWriter func(buf []byte) (int, error)

// forwardTarget 订阅端的一条写面：connID 用于下线收口，write 是本地轨写面。
type forwardTarget struct {
	connID string
	write  rtpWriter
}

// trackForwarder 把一个发布轨的一次读结果写进所有订阅端本地轨（SFU-lite
// 核心：一条读 + N 条写，避免同一 TrackRemote 被并发读）。
type trackForwarder struct {
	sessionID   string
	publisherID string
	trackID     string
	streamID    string
	codec       webrtc.RTPCodecCapability
	src         rtpReader

	mu  sync.Mutex
	dst []forwardTarget
}

// forwarderKey 是转发注册表的键：连接 ID + 轨道 ID 唯一标识一条发布轨。
func forwarderKey(f *trackForwarder) string {
	return f.publisherID + "/" + f.trackID
}

// addTarget 追加（或替换同订阅端的）写面；同一条轨重协商后换轨沿用原位置。
func (f *trackForwarder) addTarget(target forwardTarget) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.dst {
		if f.dst[i].connID == target.connID {
			f.dst[i].write = target.write
			return
		}
	}
	f.dst = append(f.dst, target)
}

// hasTarget 报告订阅端是否已挂写面（避免重复建本地轨）。
func (f *trackForwarder) hasTarget(connID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.dst {
		if t.connID == connID {
			return true
		}
	}
	return false
}

// removeTarget 撤下订阅端写面（连接下线或写面失效）。
func (f *trackForwarder) removeTarget(connID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.dst[:0]
	for _, t := range f.dst {
		if t.connID != connID {
			kept = append(kept, t)
		}
	}
	f.dst = kept
}

// targetCount 返回当前写面数（诊断与测试用）。
func (f *trackForwarder) targetCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dst)
}

// run 是转发主循环：读一个包写所有写面，源轨读面报错（发布端下线）即退出。
func (f *trackForwarder) run() {
	buf := make([]byte, rtpPacketBufferSize)
	for {
		n, err := f.src(buf)
		if n > 0 {
			f.drain(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// drain 把同一个包写进所有订阅端写面；写面报错（订阅端 PC 已关）即撤下。
func (f *trackForwarder) drain(packet []byte) {
	f.mu.Lock()
	targets := make([]forwardTarget, len(f.dst))
	copy(targets, f.dst)
	f.mu.Unlock()
	for _, target := range targets {
		if _, err := target.write(packet); err != nil {
			f.removeTarget(target.connID)
		}
	}
}

// SetStatus 更新连接状态，供 pion 状态回调 goroutine 调用。
func (c *WebRTCConnection) SetStatus(status string) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.Status = status
}

func (c *WebRTCConnection) statusValue() string {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.Status
}

// SetDataChannel 记录协商出的数据通道，供 pion OnDataChannel 回调 goroutine 调用；
// 读取方（发送/统计）持读锁取引用，消除字段读写竞争（CI race 变体实证）。
func (c *WebRTCConnection) SetDataChannel(dc *webrtc.DataChannel) {
	c.dcMu.Lock()
	defer c.dcMu.Unlock()
	c.DataChannel = dc
}

func (c *WebRTCConnection) dataChannel() *webrtc.DataChannel {
	c.dcMu.RLock()
	defer c.dcMu.RUnlock()
	return c.DataChannel
}

type WebRTCSignal struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Data      interface{} `json:"data"`
}

func NewWebRTCService(ice iceturn.ICEConfig, wsHub *WebSocketHub) *WebRTCService {
	// 创建 WebRTC API
	api := webrtc.NewAPI()

	return &WebRTCService{
		api:         api,
		connections: make(map[string]*WebRTCConnection),
		forwards:    make(map[string]*trackForwarder),
		ice:         ice,
		wsHub:       wsHub,
	}
}

// iceServers 把装配好的 ICE 配置转为 pion ICEServer 列表（纯转换，便于测试）。
func (s *WebRTCService) iceServers() []webrtc.ICEServer {
	servers := make([]webrtc.ICEServer, 0, len(s.ice.STUNServers)+1)
	for _, server := range s.ice.STUNServers {
		servers = append(servers, webrtc.ICEServer{URLs: []string{server}})
	}
	if s.ice.TURNURL != "" {
		servers = append(servers, webrtc.ICEServer{
			URLs:       []string{s.ice.TURNURL},
			Username:   s.ice.TURNUsername,
			Credential: s.ice.TURNCredential,
		})
	}
	return servers
}

// ICEConfigPayload 把装配好的 ICE 配置转为客户端下发消息体（webrtc-ice-config）。
// 每个 STUN 一项；TURN 启用时追加带 username/credential/ttl 的一项（时间限
// 凭据，客户端按 ttl 秒数规划刷新）。无任何可下发条目时返回 (nil, false)。
func (s *WebRTCService) ICEConfigPayload() (map[string]interface{}, bool) {
	entries := make([]map[string]interface{}, 0, len(s.ice.STUNServers)+1)
	for _, server := range s.ice.STUNServers {
		entries = append(entries, map[string]interface{}{"urls": []string{server}})
	}
	if s.ice.TURNURL != "" {
		entries = append(entries, map[string]interface{}{
			"urls":       []string{s.ice.TURNURL},
			"username":   s.ice.TURNUsername,
			"credential": s.ice.TURNCredential,
			// 整秒粒度；Assemble 装配出的 ICEConfig 恒为正。
			"ttl": int64(s.ice.TURNTTL / time.Second),
		})
	}
	if len(entries) == 0 {
		return nil, false
	}
	return map[string]interface{}{"ice_servers": entries}, true
}

func (s *WebRTCService) SetVoiceLifecycle(voice VoiceLifecycle) {
	s.voice = voice
}

// CreatePeerConnection 建立无信令归属的 PC（REST 面与测试用）；WS 信令路径
// 经 createPeerConnection 携带归属。
func (s *WebRTCService) CreatePeerConnection(sessionID string) (*WebRTCConnection, error) {
	return s.createPeerConnection(sessionID, "")
}

func (s *WebRTCService) createPeerConnection(sessionID string, ownerClientID string) (*WebRTCConnection, error) {
	config := webrtc.Configuration{
		ICEServers: s.iceServers(),
	}

	peerConnection, err := s.api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	connectionID := fmt.Sprintf("webrtc_%s_%d", sessionID, time.Now().UnixNano())

	conn := &WebRTCConnection{
		ID:             connectionID,
		SessionID:      sessionID,
		PeerConnection: peerConnection,
		Status:         "created",
		CreatedAt:      time.Now(),
		ownerClientID:  ownerClientID,
		svc:            s,
	}

	// 设置连接状态回调
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logrus.Infof("WebRTC connection %s state changed to %s", connectionID, state.String())
		conn.SetStatus(state.String())

		// 通知客户端状态变化（按信令归属定向投递，RA-5）
		s.sendToOwner(conn, WebSocketMessage{
			Type: "webrtc-state-change",
			Data: map[string]interface{}{
				"connection_id": connectionID,
				"state":         state.String(),
			},
		})
	})

	// 处理 ICE 候选
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}

		logrus.Infof("New ICE candidate for connection %s", connectionID)

		// ICECandidateInit 仅含可序列化标量字段，json.Marshal 不会失败，
		// 错误分支为不可达死代码。
		candidateData, _ := json.Marshal(candidate.ToJSON())

		s.sendToOwner(conn, WebSocketMessage{
			Type: "webrtc-candidate",
			Data: map[string]interface{}{
				"connection_id": connectionID,
				"candidate":     json.RawMessage(candidateData),
			},
		})
	})

	// 处理数据通道
	peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		logrus.Infof("New data channel for connection %s: %s", connectionID, dc.Label())
		conn.SetDataChannel(dc)

		dc.OnOpen(func() {
			logrus.Infof("Data channel %s opened", dc.Label())
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			logrus.Infof("Received message on data channel: %s", string(msg.Data))

			// 转发消息给客户端
			s.wsHub.SendToSession(sessionID, WebSocketMessage{
				Type: "data-channel-message",
				Data: map[string]interface{}{
					"connection_id": connectionID,
					"message":       string(msg.Data),
				},
			})
		})

		dc.OnClose(func() {
			logrus.Infof("Data channel %s closed", dc.Label())
		})
	})

	// 媒体桥接（RA-7 SFU-lite）：发布端远端轨登记转发。桥接加轨的重协商由
	// attachForwarder 显式发起（服务端 offer，客户端在同一条 PC 上应答）；
	// 不挂 OnNegotiationNeeded——数据通道等本端变更同样触发它，会向尚未支持
	// 服务端 offer 的客户端自发推 offer，打断既有信令流。
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, rtpReceiver *webrtc.RTPReceiver) {
		conn.publishTrack(track)
	})

	s.mutex.Lock()
	s.connections[connectionID] = conn
	s.mutex.Unlock()
	if s.voice != nil {
		s.voice.StartCall(context.Background(), sessionID, connectionID)
	}

	return conn, nil
}

// sendToOwner 把本连接的服务端信令帧优先投给信令归属 WS 连接（同一会话可能
// 有访客/坐席两条 PC，广播会把候选/状态发给无关对端）；无归属时回退会话广播。
func (s *WebRTCService) sendToOwner(conn *WebRTCConnection, message WebSocketMessage) {
	if conn.ownerClientID != "" {
		s.wsHub.SendToClient(conn.ownerClientID, message)
		return
	}
	s.wsHub.SendToSession(conn.SessionID, message)
}

// terminalConnStates 视为可回收的旧连接终态（断线重连场景下的残留 PC）。
var terminalConnStates = map[string]bool{
	"disconnected": true,
	"failed":       true,
	"closed":       true,
}

// closeTerminalConnections 回收同 session 终态旧 PC（RA-8 服务端半边）：
// 客户端断线重连会重发 offer，不回收则旧 PC 泄漏。存活 PC 不动——访客/
// 坐席双端各持一条 PC 的现状拓扑（RA-5 歧义）不受影响，待信令协议加
// 对端标识后再行收口。
func (s *WebRTCService) closeTerminalConnections(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for id, conn := range s.connections {
		if conn.SessionID != sessionID || !terminalConnStates[conn.statusValue()] {
			continue
		}
		if s.voice != nil {
			s.voice.EndCall(context.Background(), conn.ID)
		}
		if err := hookPeerConnectionClose(conn.PeerConnection); err != nil {
			logrus.Errorf("Failed to close stale peer connection %s: %v", id, err)
		}
		s.dropForwarderStateLocked(sessionID, conn.ID)
		delete(s.connections, id)
		logrus.Infof("Reclaimed stale WebRTC connection %s before re-offer (session %s)", id, sessionID)
	}
}

// dropForwarderStateLocked 连接下线时收口媒体桥接态（调用方已持 s.mutex）：
// 删除该连接作为发布方的转发条目（读面随 PC 关闭自然退出），撤掉它对其它
// 发布轨的订阅端写面。
func (s *WebRTCService) dropForwarderStateLocked(sessionID, connectionID string) {
	for key, fwd := range s.forwards {
		if fwd.sessionID != sessionID {
			continue
		}
		if fwd.publisherID == connectionID {
			delete(s.forwards, key)
			continue
		}
		fwd.removeTarget(connectionID)
	}
}

// registerForwarder 登记一条发布轨；同一轨重复发布是幂等 no-op（避免同一
// TrackRemote 被两条读面并发读）。
func (s *WebRTCService) registerForwarder(fwd *trackForwarder) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := forwarderKey(fwd)
	if _, exists := s.forwards[key]; exists {
		return false
	}
	s.forwards[key] = fwd
	return true
}

// forwarderCount 返回当前转发条目数（诊断与测试用）。
func (s *WebRTCService) forwarderCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.forwards)
}

// publishTrack 登记发布端远端轨并启动转发（RA-7 媒体桥接）：一次读 + N 条写，
// 新增订阅端只追加写面，不重复读轨。
func (c *WebRTCConnection) publishTrack(track *webrtc.TrackRemote) {
	c.publishForwarder(&trackForwarder{
		sessionID:   c.SessionID,
		publisherID: c.ID,
		trackID:     track.ID(),
		streamID:    track.StreamID(),
		codec:       track.Codec().RTPCodecCapability,
		src: func(buf []byte) (int, error) {
			n, _, err := track.Read(buf)
			return n, err
		},
	})
}

// publishForwarder 登记（幂等）并启动一条发布轨的转发循环，再把轨投给同会话
// 其它存活连接。
func (c *WebRTCConnection) publishForwarder(fwd *trackForwarder) {
	if !c.svc.registerForwarder(fwd) {
		return
	}
	go fwd.run() // 发布端下线时读面报错退出；注册表条目随 PC 下线收口
	c.svc.fanOutTrack(c, fwd)
}

// fanOutTrack 把新发布轨投递给同会话其它存活连接：为每个订阅端建本地轨并
// 追加写面，AddTrack 触发订阅端重协商（服务端发起 offer）。
func (s *WebRTCService) fanOutTrack(publisher *WebRTCConnection, fwd *trackForwarder) {
	s.mutex.RLock()
	targets := make([]*WebRTCConnection, 0, len(s.connections))
	for _, conn := range s.connections {
		if conn.SessionID == publisher.SessionID && conn.ID != publisher.ID && !terminalConnStates[conn.statusValue()] {
			targets = append(targets, conn)
		}
	}
	s.mutex.RUnlock()

	for _, conn := range targets {
		s.attachForwarder(conn, fwd)
	}
}

// replicateTracksTo 新建连接时补投同会话既有发布轨（晚加入者也能看到屏幕）；
// 发布方是本连接自身的条目跳过。
func (s *WebRTCService) replicateTracksTo(conn *WebRTCConnection) {
	s.mutex.RLock()
	forwards := make([]*trackForwarder, 0, len(s.forwards))
	for _, fwd := range s.forwards {
		if fwd.sessionID == conn.SessionID && fwd.publisherID != conn.ID {
			forwards = append(forwards, fwd)
		}
	}
	s.mutex.RUnlock()

	for _, fwd := range forwards {
		s.attachForwarder(conn, fwd)
	}
}

// attachForwarder 为订阅端建本地轨并追加写面；构造或 AddTrack 失败仅记日志
// （订阅端断线重连时会经 replicateTracksTo 重投）。
func (s *WebRTCService) attachForwarder(conn *WebRTCConnection, fwd *trackForwarder) {
	if fwd.hasTarget(conn.ID) {
		return
	}
	local, err := hookNewTrackLocalStaticRTP(fwd.codec, fwd.trackID, fwd.streamID)
	if err != nil {
		logrus.Errorf("Failed to create forward track %s for connection %s: %v", fwd.trackID, conn.ID, err)
		return
	}
	if _, err := conn.PeerConnection.AddTrack(local); err != nil {
		logrus.Errorf("Failed to add forward track %s to connection %s: %v", fwd.trackID, conn.ID, err)
		return
	}
	fwd.addTarget(forwardTarget{connID: conn.ID, write: local.Write})

	// 加轨即发起一次重协商，把新发送轨推给订阅端。
	go conn.negotiate()
}

// negotiate 服务端发起重协商（媒体桥接为本端新增发送轨）：创建 offer、置本地
// 描述，再把 offer 定向下发给本连接的信令归属；客户端在同一条 PC 上应答。
// 按连接串行化，避免并发本地描述操作。
func (c *WebRTCConnection) negotiate() {
	c.negotiationMu.Lock()
	defer c.negotiationMu.Unlock()

	offer, err := hookPeerConnectionCreateOffer(c.PeerConnection, nil)
	if err != nil {
		logrus.Errorf("Failed to create renegotiation offer for connection %s: %v", c.ID, err)
		return
	}
	if err := hookPeerConnectionSetLocalDescription(c.PeerConnection, offer); err != nil {
		logrus.Errorf("Failed to set renegotiation local description for connection %s: %v", c.ID, err)
		return
	}

	c.svc.sendOfferToClient(c, offer)
	logrus.Infof("Sent renegotiation offer to connection %s (session %s)", c.ID, c.SessionID)
}

// sendOfferToClient 下发重协商 offer 帧：定向下发给本连接的信令归属，无归属
// 时回退会话广播（与既有信令帧同口径）。
func (s *WebRTCService) sendOfferToClient(conn *WebRTCConnection, offer webrtc.SessionDescription) {
	s.sendToOwner(conn, WebSocketMessage{
		Type:      "webrtc-offer",
		Data:      offer,
		SessionID: conn.SessionID,
		Timestamp: time.Now(),
	})
}

func (s *WebRTCService) HandleOffer(ownerClientID string, sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	// 重连保护（RA-8 服务端半边）：应答前先回收同 session 终态旧 PC。
	s.closeTerminalConnections(sessionID)

	conn, err := s.createPeerConnection(sessionID, ownerClientID)
	if err != nil {
		return nil, err
	}

	// 设置远程描述
	err = conn.PeerConnection.SetRemoteDescription(offer)
	if err != nil {
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// 创建答案
	answer, err := hookPeerConnectionCreateAnswer(conn.PeerConnection, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// 设置本地描述
	err = hookPeerConnectionSetLocalDescription(conn.PeerConnection, answer)
	if err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	// 媒体桥接（RA-7）：补投同会话既有发布轨，晚加入者也能立即看到画面。
	s.replicateTracksTo(conn)

	logrus.Infof("Created WebRTC answer for session %s", sessionID)

	return &answer, nil
}

func (s *WebRTCService) HandleAnswer(ownerClientID string, sessionID string, answer webrtc.SessionDescription) error {
	conn, err := s.connByOwner(ownerClientID, sessionID)
	if err != nil {
		return err
	}

	err = conn.PeerConnection.SetRemoteDescription(answer)
	if err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	logrus.Infof("Set WebRTC answer for session %s", sessionID)
	if s.voice != nil {
		s.voice.AnswerCall(context.Background(), conn.ID)
	}

	return nil
}

func (s *WebRTCService) HandleICECandidate(ownerClientID string, sessionID string, candidate webrtc.ICECandidateInit) error {
	conn, err := s.connByOwner(ownerClientID, sessionID)
	if err != nil {
		return err
	}

	err = conn.PeerConnection.AddICECandidate(candidate)
	if err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	logrus.Infof("Added ICE candidate for session %s", sessionID)

	return nil
}

// connByOwner 按信令归属查找 PC（RA-5）：同一会话可能有访客/坐席两条 PC，
// 按会话首个命中会把应答/候选投给错误连接（map 遍历序随机）。无归属的旧
// 调用方（owner 为空）回退按会话首个命中，保持既有行为。
func (s *WebRTCService) connByOwner(ownerClientID, sessionID string) (*WebRTCConnection, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if ownerClientID != "" {
		for _, conn := range s.connections {
			if conn.SessionID == sessionID && conn.ownerClientID == ownerClientID {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("connection not found for session %s", sessionID)
	}

	return s.firstConnectionBySessionID(sessionID)
}

// firstConnectionBySessionID 按会话取首个命中连接（仅供无归属路径回退）；
// 调用方必须已持 s.mutex（读锁）。
func (s *WebRTCService) firstConnectionBySessionID(sessionID string) (*WebRTCConnection, error) {
	for _, conn := range s.connections {
		if conn.SessionID == sessionID {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("connection not found for session %s", sessionID)
}

func (s *WebRTCService) CloseConnection(sessionID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for id, conn := range s.connections {
		if conn.SessionID == sessionID {
			if s.voice != nil {
				s.voice.EndCall(context.Background(), conn.ID)
			}
			err := hookPeerConnectionClose(conn.PeerConnection)
			if err != nil {
				logrus.Errorf("Failed to close peer connection %s: %v", id, err)
			}
			s.dropForwarderStateLocked(sessionID, conn.ID)
			delete(s.connections, id)
			logrus.Infof("Closed WebRTC connection %s", id)
		}
	}

	return nil
}

func (s *WebRTCService) getConnectionBySessionID(sessionID string) (*WebRTCConnection, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.firstConnectionBySessionID(sessionID)
}

func (s *WebRTCService) GetConnectionStats(sessionID string) (map[string]interface{}, error) {
	conn, err := s.getConnectionBySessionID(sessionID)
	if err != nil {
		return nil, err
	}

	// 获取连接基本信息
	connState := conn.PeerConnection.ConnectionState()
	iceConnState := conn.PeerConnection.ICEConnectionState()
	iceGatheringState := conn.PeerConnection.ICEGatheringState()

	statsMap := map[string]interface{}{
		"connection_id":        conn.ID,
		"session_id":           conn.SessionID,
		"connection_state":     connState.String(),
		"ice_connection_state": iceConnState.String(),
		"ice_gathering_state":  iceGatheringState.String(),
		"created_at":           conn.CreatedAt,
		"status":               conn.statusValue(),
	}

	// 获取数据通道信息
	if dc := conn.dataChannel(); dc != nil {
		statsMap["data_channel"] = map[string]interface{}{
			"label":       dc.Label(),
			"ready_state": dc.ReadyState().String(),
		}
	}

	return statsMap, nil
}

func (s *WebRTCService) SendDataChannelMessage(sessionID, message string) error {
	conn, err := s.getConnectionBySessionID(sessionID)
	if err != nil {
		return err
	}

	dc := conn.dataChannel()
	if dc == nil {
		return fmt.Errorf("data channel not available for session %s", sessionID)
	}

	err = dc.SendText(message)
	if err != nil {
		return fmt.Errorf("failed to send data channel message: %w", err)
	}

	return nil
}

func (s *WebRTCService) GetConnectionCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.connections)
}
