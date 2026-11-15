package realtime

import (
	"servify/apps/server/internal/platform/iceturn"
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWebRTCAdapter() *WebRTCAdapter {
	hub := NewWebSocketHub()
	go hub.Run()
	return NewWebRTCAdapter(NewWebRTCService(iceturn.ICEConfig{STUNServers: []string{"stun:127.0.0.1:1"}}, hub))
}

func TestWebRTCAdapter_MissingSessionErrors(t *testing.T) {
	adapter := newTestWebRTCAdapter()

	stats, err := adapter.ConnectionStats("missing-session")
	require.Error(t, err)
	assert.Nil(t, stats)
	assert.Equal(t, 0, adapter.ConnectionCount())

	require.Error(t, adapter.HandleAnswer("missing-session", webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "sdp"}))
	require.Error(t, adapter.HandleICECandidate("missing-session", webrtc.ICECandidateInit{Candidate: "candidate"}))
	require.NoError(t, adapter.CloseConnection("missing-session"))
}

func TestWebRTCAdapter_HandleOffer(t *testing.T) {
	adapter := newTestWebRTCAdapter()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()
	_, err = pc.CreateDataChannel("test", nil)
	require.NoError(t, err)
	offer, err := pc.CreateOffer(nil)
	require.NoError(t, err)
	require.NotEmpty(t, offer.SDP)

	answer, err := adapter.HandleOffer("session-1", offer)
	require.NoError(t, err)
	require.NotNil(t, answer)
	assert.Equal(t, webrtc.SDPTypeAnswer, answer.Type)
	assert.NotEmpty(t, answer.SDP)
	assert.Equal(t, 1, adapter.ConnectionCount())
}

func TestWebRTCAdapter_ConnectionStats(t *testing.T) {
	adapter := newTestWebRTCAdapter()

	// 畸形 SDP：协商失败即回滚，连接表不得残留占位连接
	_, err := adapter.HandleOffer("session-1", webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "not-a-valid-sdp"})
	require.Error(t, err)
	_, statsErr := adapter.ConnectionStats("session-1")
	assert.Error(t, statsErr)
	assert.Equal(t, 0, adapter.ConnectionCount())

	// 合法 offer 后统计可用，初始状态如实为 created
	valid := validTestOffer(t)
	answer, err := adapter.HandleOffer("session-1", valid)
	require.NoError(t, err)
	require.NotNil(t, answer)

	stats, err := adapter.ConnectionStats("session-1")
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, "session-1", stats["session_id"])
	assert.NotEmpty(t, stats["connection_id"])
	// 状态由 pion 回调异步推进（created→connecting→…），断言字段在场而非瞬时值
	assert.NotEmpty(t, stats["status"])
}
