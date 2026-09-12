package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

type stubRTCGateway struct {
	stats map[string]interface{}
	err   error
	count int
}

func (s *stubRTCGateway) ConnectionStats(sessionID string) (map[string]interface{}, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]interface{}{
		"session_id": sessionID,
	}
	for k, v := range s.stats {
		out[k] = v
	}
	return out, nil
}

func (s *stubRTCGateway) ConnectionCount() int {
	return s.count
}

func (s *stubRTCGateway) HandleOffer(string, webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	return nil, nil
}

func (s *stubRTCGateway) HandleAnswer(string, webrtc.SessionDescription) error {
	return nil
}

func (s *stubRTCGateway) HandleICECandidate(string, webrtc.ICECandidateInit) error {
	return nil
}

func (s *stubRTCGateway) CloseConnection(string) error {
	return nil
}

func TestWebRTCHandlerGetStatsReturnsAggregateWithoutSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewWebRTCHandler(&stubRTCGateway{count: 3})
	router := gin.New()
	router.GET("/api/v1/webrtc/stats", handler.GetStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		Success bool `json:"success"`
		Data    struct {
			ConnectionCount int    `json:"connection_count"`
			Scope           string `json:"scope"`
			Status          string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}

	if !got.Success {
		t.Fatalf("expected success=true body=%s", w.Body.String())
	}
	if got.Data.ConnectionCount != 3 {
		t.Fatalf("expected connection_count=3 got %d", got.Data.ConnectionCount)
	}
	if got.Data.Scope != "all" {
		t.Fatalf("expected scope=all got %q", got.Data.Scope)
	}
	if got.Data.Status != "running" {
		t.Fatalf("expected status=running got %q", got.Data.Status)
	}
}

func TestWebRTCHandlerGetStatsReturnsSessionDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewWebRTCHandler(&stubRTCGateway{
		count: 1,
		stats: map[string]interface{}{
			"connection_state": "connected",
		},
	})
	router := gin.New()
	router.GET("/api/v1/webrtc/stats", handler.GetStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/stats?session_id=sess-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got struct {
		Success bool `json:"success"`
		Data    struct {
			SessionID       string `json:"session_id"`
			ConnectionState string `json:"connection_state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}

	if !got.Success {
		t.Fatalf("expected success=true body=%s", w.Body.String())
	}
	if got.Data.SessionID != "sess-1" {
		t.Fatalf("expected session_id=sess-1 got %q", got.Data.SessionID)
	}
	if got.Data.ConnectionState != "connected" {
		t.Fatalf("expected connection_state=connected got %q", got.Data.ConnectionState)
	}
}

func TestWebRTCHandlerGetStatsReturnsErrorForFailedLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewWebRTCHandler(&stubRTCGateway{
		err: errors.New("lookup failed"),
	})
	router := gin.New()
	router.GET("/api/v1/webrtc/stats", handler.GetStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/stats?session_id=sess-404", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", w.Code, w.Body.String())
	}
}
