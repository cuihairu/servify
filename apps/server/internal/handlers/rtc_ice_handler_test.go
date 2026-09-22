package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type stubICEConfigSource struct {
	payload map[string]interface{}
	ok      bool
}

func (s stubICEConfigSource) ICEConfigPayload() (map[string]interface{}, bool) {
	return s.payload, s.ok
}

func performIceServersRequest(t *testing.T, handler *RTCICEHandler) map[string]interface{} {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/rtc/ice-servers", handler.GetIceServers)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rtc/ice-servers", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestRTCICEHandlerReturnsAssembledPayload(t *testing.T) {
	payload := map[string]interface{}{
		"ice_servers": []map[string]interface{}{
			{"urls": []string{"stun:stun.example.com:3478"}},
			{
				"urls":       []string{"turn:turn.example.com:3478"},
				"username":   "1790000300",
				"credential": "isReWBKNlmmMSS3VR4Xr9PtPqYE=",
				"ttl":        int64(300),
			},
		},
	}
	body := performIceServersRequest(t, NewRTCICEHandler(stubICEConfigSource{payload: payload, ok: true}))
	if body["success"] != true {
		t.Fatalf("success = %v, want true", body["success"])
	}
	data := body["data"].(map[string]interface{})
	servers := data["ice_servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("ice_servers length = %d, want 2", len(servers))
	}
	turn := servers[1].(map[string]interface{})
	if turn["username"] != "1790000300" || turn["ttl"] != float64(300) {
		t.Fatalf("turn entry = %+v, want username/ttl carried over", turn)
	}
}

func TestRTCICEHandlerEmptyConfigServesEmptyList(t *testing.T) {
	// STUN/TURN 均未启用时 ok=false；REST 面仍应可答（空列表），客户端回退宿主自备。
	body := performIceServersRequest(t, NewRTCICEHandler(stubICEConfigSource{payload: nil, ok: false}))
	data := body["data"].(map[string]interface{})
	if servers, exists := data["ice_servers"]; !exists || len(servers.([]interface{})) != 0 {
		t.Fatalf("ice_servers = %+v, want empty list", data["ice_servers"])
	}
}

func TestRTCICEHandlerUnwiredGatewayIs503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/rtc/ice-servers", NewRTCICEHandler(nil).GetIceServers)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rtc/ice-servers", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
