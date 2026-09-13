package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/sirupsen/logrus"
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
	stunServer  string
	wsHub       *WebSocketHub
	voice       VoiceLifecycle
}

type WebRTCConnection struct {
	ID             string
	SessionID      string
	PeerConnection *webrtc.PeerConnection
	DataChannel    *webrtc.DataChannel
	Status         string
	CreatedAt      time.Time

	statusMu sync.RWMutex
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

type WebRTCSignal struct {
	Type      string      `json:"type"`
	SessionID string      `json:"session_id"`
	Data      interface{} `json:"data"`
}

func NewWebRTCService(stunServer string, wsHub *WebSocketHub) *WebRTCService {
	// 创建 WebRTC API
	api := webrtc.NewAPI()

	return &WebRTCService{
		api:         api,
		connections: make(map[string]*WebRTCConnection),
		stunServer:  stunServer,
		wsHub:       wsHub,
	}
}

func (s *WebRTCService) SetVoiceLifecycle(voice VoiceLifecycle) {
	s.voice = voice
}

func (s *WebRTCService) CreatePeerConnection(sessionID string) (*WebRTCConnection, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{s.stunServer},
			},
		},
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
	}

	// 设置连接状态回调
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logrus.Infof("WebRTC connection %s state changed to %s", connectionID, state.String())
		conn.SetStatus(state.String())

		// 通知客户端状态变化
		s.wsHub.SendToSession(sessionID, WebSocketMessage{
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

		s.wsHub.SendToSession(sessionID, WebSocketMessage{
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
		conn.DataChannel = dc

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

	s.mutex.Lock()
	s.connections[connectionID] = conn
	s.mutex.Unlock()
	if s.voice != nil {
		s.voice.StartCall(context.Background(), sessionID, connectionID)
	}

	return conn, nil
}

func (s *WebRTCService) HandleOffer(sessionID string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	conn, err := s.CreatePeerConnection(sessionID)
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

	logrus.Infof("Created WebRTC answer for session %s", sessionID)

	return &answer, nil
}

func (s *WebRTCService) HandleAnswer(sessionID string, answer webrtc.SessionDescription) error {
	conn, err := s.getConnectionBySessionID(sessionID)
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

func (s *WebRTCService) HandleICECandidate(sessionID string, candidate webrtc.ICECandidateInit) error {
	conn, err := s.getConnectionBySessionID(sessionID)
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
			delete(s.connections, id)
			logrus.Infof("Closed WebRTC connection %s", id)
		}
	}

	return nil
}

func (s *WebRTCService) getConnectionBySessionID(sessionID string) (*WebRTCConnection, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for _, conn := range s.connections {
		if conn.SessionID == sessionID {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("connection not found for session %s", sessionID)
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
	if conn.DataChannel != nil {
		statsMap["data_channel"] = map[string]interface{}{
			"label":       conn.DataChannel.Label(),
			"ready_state": conn.DataChannel.ReadyState().String(),
		}
	}

	return statsMap, nil
}

func (s *WebRTCService) SendDataChannelMessage(sessionID, message string) error {
	conn, err := s.getConnectionBySessionID(sessionID)
	if err != nil {
		return err
	}

	if conn.DataChannel == nil {
		return fmt.Errorf("data channel not available for session %s", sessionID)
	}

	err = conn.DataChannel.SendText(message)
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
