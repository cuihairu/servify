package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	twiliovoice "servify/apps/server/internal/platform/twiliovoice"

	"github.com/gin-gonic/gin"
)

const pstnTestToken = "test-auth-token"

var pstnSignedURL = "https://servify.example.com/public/voice/webhooks/twilio"

// twilioTestSignature mirrors the vendor algorithm over the full URL plus
// sorted form parameters (same material as the adapter validates against).
func twilioTestSignature(token, requestURL string, form map[string]string) string {
	keys := make([]string, 0, len(form))
	for key := range form {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(requestURL)
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString(form[key])
	}
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write([]byte(b.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func newPSTNFixture(validateSignature bool) (*gin.Engine, *voiceinfra.InMemoryRepository, *voiceinfra.InMemoryRecordingRepository) {
	calls := voiceinfra.NewInMemoryRepository()
	recordings := voiceinfra.NewInMemoryRecordingRepository()
	bus := &voiceTestBus{}
	coordinator := voicedelivery.NewCoordinator(
		voiceapp.NewService(calls, bus),
		voiceapp.NewRecordingService(voiceprovidermock.NewRecordingProvider(), recordings, bus),
		voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), voiceinfra.NewInMemoryTranscriptRepository(), bus),
	)
	router := gin.New()
	public := router.Group("/public")
	handler := NewPSTNWebhookHandler(coordinator, twiliovoice.NewAdapter(pstnTestToken), validateSignature, "https://servify.example.com")
	RegisterPSTNWebhookRoutes(public, handler)
	return router, calls, recordings
}

func postPSTNWebhook(t *testing.T, router *gin.Engine, form map[string]string, signature string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{}
	for key, value := range form {
		values.Set(key, value)
	}
	req := httptest.NewRequest(http.MethodPost, "/public/voice/webhooks/twilio", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if signature != "" {
		req.Header.Set("X-Twilio-Signature", signature)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestPSTNWebhookRejectsBadSignature(t *testing.T) {
	router, _, _ := newPSTNFixture(true)
	form := map[string]string{"CallSid": "CA-bad", "CallStatus": "queued"}
	resp := postPSTNWebhook(t, router, form, twilioTestSignature("wrong-token", pstnSignedURL, form))
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", resp.Code, resp.Body.String())
	}
}

func TestPSTNWebhookSignatureCanBeDisabled(t *testing.T) {
	router, _, _ := newPSTNFixture(false)
	form := map[string]string{"CallSid": "CA-open", "CallStatus": "queued"}
	resp := postPSTNWebhook(t, router, form, "garbage-signature")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with validation disabled, body=%s", resp.Code, resp.Body.String())
	}
}

func TestPSTNWebhookInviteAndHangup(t *testing.T) {
	router, calls, _ := newPSTNFixture(true)

	invite := map[string]string{"CallSid": "CA-life", "CallStatus": "queued", "From": "+100", "To": "+200"}
	resp := postPSTNWebhook(t, router, invite, twilioTestSignature(pstnTestToken, pstnSignedURL, invite))
	if resp.Code != http.StatusOK {
		t.Fatalf("invite status = %d, body=%s", resp.Code, resp.Body.String())
	}
	call, ok := calls.GetCall("CA-life")
	if !ok {
		t.Fatal("call not persisted after invite")
	}
	if call.Status != "started" {
		t.Fatalf("call status = %s, want started", call.Status)
	}

	completed := map[string]string{"CallSid": "CA-life", "CallStatus": "completed"}
	resp = postPSTNWebhook(t, router, completed, twilioTestSignature(pstnTestToken, pstnSignedURL, completed))
	if resp.Code != http.StatusOK {
		t.Fatalf("completed status = %d, body=%s", resp.Code, resp.Body.String())
	}
	call, _ = calls.GetCall("CA-life")
	if call.Status != "ended" {
		t.Fatalf("call status = %s, want ended", call.Status)
	}
}

func TestPSTNWebhookDuplicateInviteIsIdempotent(t *testing.T) {
	router, calls, _ := newPSTNFixture(true)
	form := map[string]string{"CallSid": "CA-dup", "CallStatus": "queued"}
	for i := 0; i < 2; i++ {
		resp := postPSTNWebhook(t, router, form, twilioTestSignature(pstnTestToken, pstnSignedURL, form))
		if resp.Code != http.StatusOK {
			t.Fatalf("invite #%d status = %d, body=%s", i, resp.Code, resp.Body.String())
		}
	}
	if _, ok := calls.GetCall("CA-dup"); !ok {
		t.Fatal("call not persisted")
	}
}

func TestPSTNWebhookRecordingStoredWithStorageURI(t *testing.T) {
	router, _, recordings := newPSTNFixture(true)
	form := map[string]string{
		"CallSid":         "CA-rec",
		"RecordingSid":    "RE-rec",
		"RecordingUrl":    "https://api.twilio.com/recordings/RE-rec.mp3",
		"RecordingStatus": "completed",
	}
	resp := postPSTNWebhook(t, router, form, twilioTestSignature(pstnTestToken, pstnSignedURL, form))
	if resp.Code != http.StatusOK {
		t.Fatalf("recording status = %d, body=%s", resp.Code, resp.Body.String())
	}
	rec, err := recordings.FindByID(context.Background(), "RE-rec")
	if err != nil || rec == nil {
		t.Fatalf("FindByID() = %+v, %v", rec, err)
	}
	if rec.StorageURI != form["RecordingUrl"] {
		t.Fatalf("StorageURI = %q, want %q", rec.StorageURI, form["RecordingUrl"])
	}
}

// provider disabled ⇒ 没有注册的 hosted adapter ⇒ 路由不存在,匿名 404。
func TestPSTNWebhookRouteAbsentWithoutAdapter(t *testing.T) {
	router := gin.New()
	RegisterPSTNWebhookRoutes(router.Group("/public"), nil)
	resp := postPSTNWebhook(t, router, map[string]string{"CallSid": "CA-x"}, "")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Code)
	}
}
