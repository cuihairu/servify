package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"servify/apps/server/internal/services"
)

func TestDefaultTimestamp_ZeroBecomesNow(t *testing.T) {
	result := defaultTimestamp(time.Time{})
	assert.False(t, result.IsZero())
}

func TestDefaultTimestamp_NonZeroPreserved(t *testing.T) {
	fixed := time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC)
	assert.Equal(t, fixed, defaultTimestamp(fixed))
}

func TestWebSocketAdapter_ClientCountWithoutClients(t *testing.T) {
	adapter := NewWebSocketAdapter(services.NewWebSocketHub())
	assert.Equal(t, 0, adapter.ClientCount())
}

func TestWebSocketAdapter_HandleWebSocketRequiresSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := services.NewWebSocketHub()
	go hub.Run()
	adapter := NewWebSocketAdapter(hub)

	router := gin.New()
	router.GET("/ws", adapter.HandleWebSocket)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWebSocketAdapter_HandleWebSocketAndSendToSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := services.NewWebSocketHub()
	go hub.Run()
	adapter := NewWebSocketAdapter(hub)

	router := gin.New()
	router.GET("/ws", adapter.HandleWebSocket)
	srv := httptest.NewServer(router)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?session_id=session-1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "handshake status %v", resp)
	defer conn.Close()

	require.Eventually(t, func() bool {
		return adapter.ClientCount() == 1
	}, 3*time.Second, 10*time.Millisecond, "client should be registered")

	adapter.SendToSession("session-1", Message{Type: "greeting", Data: "hello"})

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	var msg services.WebSocketMessage
	require.NoError(t, conn.ReadJSON(&msg))
	assert.Equal(t, "greeting", msg.Type)
	assert.Equal(t, "hello", msg.Data)
	assert.Equal(t, "session-1", msg.SessionID)
	assert.False(t, msg.Timestamp.IsZero())
}
