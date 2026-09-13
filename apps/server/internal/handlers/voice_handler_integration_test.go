//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	"servify/apps/server/internal/platform/pstnprovider"
	"servify/apps/server/internal/platform/sip"
	"servify/apps/server/internal/platform/sipws"
	twiliovoice "servify/apps/server/internal/platform/twiliovoice"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
)

type voiceIntegrationFixture struct {
	router *gin.Engine
	repo   *voiceinfra.InMemoryRepository
}

func newVoiceIntegrationFixture() voiceIntegrationFixture {
	repo := voiceinfra.NewInMemoryRepository()
	bus := &voiceTestBus{}
	coordinator := voicedelivery.NewCoordinator(
		voiceapp.NewService(repo, bus),
		voiceapp.NewRecordingService(voiceprovidermock.NewRecordingProvider(), voiceinfra.NewInMemoryRecordingRepository(), bus),
		voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), voiceinfra.NewInMemoryTranscriptRepository(), bus),
	)
	registry := voiceprotocol.NewRegistry()
	_ = registry.RegisterSignaling(sip.NewVoiceProtocolAdapter())
	_ = registry.RegisterSignaling(sipws.NewAdapter())
	_ = registry.RegisterSignaling(pstnprovider.NewAdapter())
	_ = registry.RegisterSignaling(twiliovoice.NewAdapter("integration-test-token"))
	_ = registry.RegisterMedia(voicedelivery.NewWebRTCAdapter(voiceapp.NewService(voiceinfra.NewInMemoryRepository(), bus)))
	_ = registry.RegisterMedia(voicedelivery.NewRTPAdapter())
	_ = registry.RegisterMedia(voicedelivery.NewSRTPAdapter())

	router := gin.New()
	api := router.Group("/api")
	RegisterVoiceRoutes(api, NewVoiceHandler(coordinator, registry))
	public := router.Group("/public")
	RegisterPSTNWebhookRoutes(public, NewPSTNWebhookHandler(coordinator, twiliovoice.NewAdapter("integration-test-token"), true, "https://integration.example.com"))

	return voiceIntegrationFixture{
		router: router,
		repo:   repo,
	}
}

func (f voiceIntegrationFixture) post(t *testing.T, path string, payload map[string]interface{}, expected int) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	f.router.ServeHTTP(resp, req)
	if resp.Code != expected {
		t.Fatalf("%s expected %d, got %d, body=%s", path, expected, resp.Code, resp.Body.String())
	}
}

func (f voiceIntegrationFixture) get(t *testing.T, path string, expected int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp := httptest.NewRecorder()
	f.router.ServeHTTP(resp, req)
	if resp.Code != expected {
		t.Fatalf("%s expected %d, got %d, body=%s", path, expected, resp.Code, resp.Body.String())
	}
	return resp
}

func TestVoiceHandlerSIPProtocolLifecycleIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	callPayload := map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id": "call-sip-42",
			"from":    "1001",
			"to":      "1002",
		},
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/invite", callPayload, http.StatusOK)
	call, ok := fixture.repo.GetCall("call-sip-42")
	if !ok || call.Status != "started" {
		t.Fatalf("expected started SIP call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/answer", callPayload, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-sip-42")
	if !ok || call.Status != "answered" || call.AnsweredAt == nil {
		t.Fatalf("expected answered SIP call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/hangup", callPayload, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-sip-42")
	if !ok || call.Status != "ended" || call.EndedAt == nil {
		t.Fatalf("expected ended SIP call, got ok=%v call=%+v", ok, call)
	}
}

func TestVoiceHandlerSIPWebSocketProtocolLifecycleIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	payload := map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id":       "call-sipws-7",
			"connection_id": "conn-7",
			"from":          "2001",
			"to":            "2002",
			"method":        "INVITE",
		},
	}
	fixture.post(t, "/api/voice/protocols/sip-ws/call-events/invite", payload, http.StatusOK)
	call, ok := fixture.repo.GetCall("call-sipws-7")
	if !ok || call.Status != "started" {
		t.Fatalf("expected started sip-ws call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip-ws/call-events/dtmf", map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id":       "call-sipws-7",
			"connection_id": "conn-7",
			"from":          "2001",
			"to":            "2002",
			"method":        "INFO",
			"dtmf":          "5",
		},
	}, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-sipws-7")
	if !ok || call.Status != "started" {
		t.Fatalf("expected sip-ws call to remain active after dtmf, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip-ws/call-events/hangup", payload, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-sipws-7")
	if !ok || call.Status != "ended" {
		t.Fatalf("expected ended sip-ws call, got ok=%v call=%+v", ok, call)
	}
}

func TestVoiceHandlerPSTNProviderProtocolLifecycleIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	payload := map[string]interface{}{
		"payload": map[string]interface{}{
			"provider":   "mock-pstn",
			"event_id":   "evt-1",
			"event_type": "invite",
			"call_id":    "call-pstn-9",
			"from":       "+8613800000000",
			"to":         "+8613900000000",
		},
	}
	fixture.post(t, "/api/voice/protocols/pstn-provider/call-events/invite", payload, http.StatusOK)
	call, ok := fixture.repo.GetCall("call-pstn-9")
	if !ok || call.Status != "started" {
		t.Fatalf("expected started pstn-provider call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/pstn-provider/call-events/transfer", map[string]interface{}{
		"payload": map[string]interface{}{
			"provider":   "mock-pstn",
			"event_id":   "evt-2",
			"event_type": "transfer",
			"call_id":    "call-pstn-9",
			"from":       "+8613800000000",
			"to":         "+8613900000000",
			"metadata": map[string]interface{}{
				"target_agent_id": 7,
			},
		},
	}, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-pstn-9")
	if !ok || call.Status != "transferred" || call.TransferToAgent == nil || *call.TransferToAgent != 7 {
		t.Fatalf("expected transferred pstn-provider call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/pstn-provider/call-events/hangup", payload, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-pstn-9")
	if !ok || call.Status != "ended" {
		t.Fatalf("expected ended pstn-provider call, got ok=%v call=%+v", ok, call)
	}
}

func TestVoiceHandlerProtocolsContractIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	resp := fixture.get(t, "/api/voice/protocols", http.StatusOK)
	body := resp.Body.String()
	for _, protocol := range []string{"pstn-provider", "sip", "sip-ws", "webrtc", "rtp", "srtp"} {
		if !bytes.Contains(resp.Body.Bytes(), []byte(protocol)) {
			t.Fatalf("expected protocol %q in response body: %s", protocol, body)
		}
	}
}

func TestVoiceHandlerCallControlSemanticsIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	payload := map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id": "call-semantics-1",
			"from":    "3001",
			"to":      "3002",
		},
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/invite", payload, http.StatusOK)
	fixture.post(t, "/api/voice/protocols/sip/call-events/hold", payload, http.StatusOK)
	call, ok := fixture.repo.GetCall("call-semantics-1")
	if !ok || call.Status != "held" || call.HeldAt == nil {
		t.Fatalf("expected held call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/resume", payload, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-semantics-1")
	if !ok || call.Status != "answered" || call.ResumedAt == nil {
		t.Fatalf("expected resumed call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/transfer", map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id": "call-semantics-1",
			"from":    "3001",
			"to":      "3002",
			"metadata": map[string]interface{}{
				"target_agent_id": "9",
			},
		},
	}, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-semantics-1")
	if !ok || call.Status != "transferred" || call.TransferToAgent == nil || *call.TransferToAgent != 9 {
		t.Fatalf("expected transferred call, got ok=%v call=%+v", ok, call)
	}

	fixture.post(t, "/api/voice/protocols/sip/call-events/dtmf", map[string]interface{}{
		"payload": map[string]interface{}{
			"call_id": "call-semantics-1",
			"from":    "3001",
			"to":      "3002",
			"dtmf":    "8",
		},
	}, http.StatusOK)
	call, ok = fixture.repo.GetCall("call-semantics-1")
	if !ok || call.Status != "transferred" {
		t.Fatalf("expected dtmf not to mutate call state directly, got ok=%v call=%+v", ok, call)
	}
}

// postPSTNForm posts a form-encoded vendor status callback carrying a valid
// X-Twilio-Signature for the fixture's adapter token.
func (f voiceIntegrationFixture) postPSTNForm(t *testing.T, form map[string]string, expected int) {
	t.Helper()
	values := url.Values{}
	for key, value := range form {
		values.Set(key, value)
	}
	req := httptest.NewRequest(http.MethodPost, "/public/voice/webhooks/twilio", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", twilioTestSignature("integration-test-token", "https://integration.example.com/public/voice/webhooks/twilio", form))
	resp := httptest.NewRecorder()
	f.router.ServeHTTP(resp, req)
	if resp.Code != expected {
		t.Fatalf("pstn webhook expected %d, got %d, body=%s", expected, resp.Code, resp.Body.String())
	}
}

func TestVoiceHandlerPSTNWebhookLifecycleIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := newVoiceIntegrationFixture()

	fixture.postPSTNForm(t, map[string]string{"CallSid": "CA-tw-1", "CallStatus": "queued"}, http.StatusOK)
	call, ok := fixture.repo.GetCall("CA-tw-1")
	if !ok || call.Status != "started" {
		t.Fatalf("expected started PSTN call, got ok=%v call=%+v", ok, call)
	}

	fixture.postPSTNForm(t, map[string]string{"CallSid": "CA-tw-1", "CallStatus": "in-progress"}, http.StatusOK)
	call, _ = fixture.repo.GetCall("CA-tw-1")
	if !ok || call.Status != "answered" {
		t.Fatalf("expected answered PSTN call, got ok=%v call=%+v", ok, call)
	}

	fixture.postPSTNForm(t, map[string]string{"CallSid": "CA-tw-1", "CallStatus": "completed"}, http.StatusOK)
	call, _ = fixture.repo.GetCall("CA-tw-1")
	if !ok || call.Status != "ended" || call.EndedAt == nil {
		t.Fatalf("expected ended PSTN call, got ok=%v call=%+v", ok, call)
	}

	// 迟到的重复 queued 回调:幂等短路,call 状态不再回退。
	fixture.postPSTNForm(t, map[string]string{"CallSid": "CA-tw-1", "CallStatus": "queued"}, http.StatusOK)
	call, _ = fixture.repo.GetCall("CA-tw-1")
	if call.Status != "ended" {
		t.Fatalf("expected duplicate invite to short-circuit, got %s", call.Status)
	}
}
