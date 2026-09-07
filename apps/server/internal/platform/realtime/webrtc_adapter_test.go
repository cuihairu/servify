package realtime

import (
	"testing"

	"github.com/pion/webrtc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/services"
)

func newTestWebRTCAdapter() *WebRTCAdapter {
	hub := services.NewWebSocketHub()
	go hub.Run()
	return NewWebRTCAdapter(services.NewWebRTCService("stun:127.0.0.1:1", hub))
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

	_, err := adapter.HandleOffer("session-1", webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: "not-a-valid-sdp"})
	require.Error(t, err)

	stats, err := adapter.ConnectionStats("session-1")
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, "session-1", stats["session_id"])
	assert.NotEmpty(t, stats["connection_id"])
	assert.Equal(t, "created", stats["status"])
}
