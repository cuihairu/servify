package handlers

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// cvhPSTNAdapter 完全可控的 hosted vendor webhook 适配器 mock。
type cvhPSTNAdapter struct {
	sigErr      error
	mapErr      error
	callEvents  []voiceprotocol.CallEvent
	mediaEvents []voiceprotocol.MediaEvent
}

func (a *cvhPSTNAdapter) Name() string { return "cvh-pstn" }
func (a *cvhPSTNAdapter) Protocol() voiceprotocol.Protocol {
	return voiceprotocol.ProtocolHostedVendorWebhook
}
func (a *cvhPSTNAdapter) WebhookPath() string { return "/public/voice/webhooks/cvh" }
func (a *cvhPSTNAdapter) MapInvite(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapAnswer(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapHold(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapResume(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapHangup(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapTransfer(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) MapDTMF(context.Context, any) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, nil
}
func (a *cvhPSTNAdapter) ValidateSignature(context.Context, string, map[string]string, []byte) error {
	return a.sigErr
}
func (a *cvhPSTNAdapter) MapWebhook(context.Context, any) ([]voiceprotocol.CallEvent, []voiceprotocol.MediaEvent, error) {
	return a.callEvents, a.mediaEvents, a.mapErr
}

var _ voiceprotocol.HostedVendorWebhookAdapter = (*cvhPSTNAdapter)(nil)

func cvhPSTNRouter(t *testing.T, adapter voiceprotocol.HostedVendorWebhookAdapter, validateSig bool, base string) *gin.Engine {
	t.Helper()
	calls := voiceinfra.NewInMemoryRepository()
	recordings := voiceinfra.NewInMemoryRecordingRepository()
	bus := &voiceTestBus{}
	coordinator := voicedelivery.NewCoordinator(
		voiceapp.NewService(calls, bus),
		voiceapp.NewRecordingService(voiceprovidermock.NewRecordingProvider(), recordings, bus),
		voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), voiceinfra.NewInMemoryTranscriptRepository(), bus),
	)
	r := gin.New()
	RegisterPSTNWebhookRoutes(r.Group("/public"), NewPSTNWebhookHandler(coordinator, adapter, validateSig, base))
	return r
}

func cvhPostPSTNRaw(t *testing.T, r *gin.Engine, path, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCvhPSTNWebhookRuntimeUnavailable(t *testing.T) {
	r := gin.New()
	RegisterPSTNWebhookRoutes(r.Group("/public"), NewPSTNWebhookHandler(nil, &cvhPSTNAdapter{}, false, ""))
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "pstn webhook runtime unavailable")
}

func TestCvhPSTNWebhookBodyReadError(t *testing.T) {
	r := cvhPSTNRouter(t, &cvhPSTNAdapter{}, false, "")
	req := httptest.NewRequest(http.MethodPost, "/public/voice/webhooks/cvh", nil)
	req.Body = io.NopCloser(iotest.ErrReader(errors.New("socket reset")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "read request body")
}

func TestCvhPSTNWebhookSignatureRejected(t *testing.T) {
	adapter := &cvhPSTNAdapter{sigErr: errors.New("bad signature")}
	// publicBaseURL 为空：requestURL 走请求自身 scheme/host 兜底（http 分支）
	r := cvhPSTNRouter(t, adapter, true, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "webhook signature rejected")

	// X-Forwarded-Proto 优先于请求 scheme
	w = cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", func(req *http.Request) {
		req.Header.Set("X-Forwarded-Proto", "https")
	})
	assert.Equal(t, http.StatusForbidden, w.Code)

	// TLS 请求 → https 兜底
	w = cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", func(req *http.Request) {
		req.TLS = &tls.ConnectionState{}
	})
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCvhPSTNWebhookMalformedForm(t *testing.T) {
	r := cvhPSTNRouter(t, &cvhPSTNAdapter{}, false, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "%zz=broken", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "parse form body")
}

func TestCvhPSTNWebhookMapError(t *testing.T) {
	adapter := &cvhPSTNAdapter{mapErr: errors.New("unmapped payload")}
	r := cvhPSTNRouter(t, adapter, false, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unmapped payload")
}

func TestCvhPSTNWebhookCallEventRejected(t *testing.T) {
	adapter := &cvhPSTNAdapter{callEvents: []voiceprotocol.CallEvent{{
		EventID:  "e1",
		Kind:     voiceprotocol.CallEventKind("bogus"),
		CallID:   "c1",
		Protocol: voiceprotocol.ProtocolHostedVendorWebhook,
	}}}
	r := cvhPSTNRouter(t, adapter, false, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported call event kind")
}

func TestCvhPSTNWebhookMediaEventRejected(t *testing.T) {
	adapter := &cvhPSTNAdapter{mediaEvents: []voiceprotocol.MediaEvent{{
		EventID:  "e2",
		Kind:     voiceprotocol.MediaEventRecordingStop,
		CallID:   "c1",
		Protocol: voiceprotocol.ProtocolHostedVendorWebhook,
		Metadata: map[string]any{"recording_id": "missing-recording"},
	}}}
	r := cvhPSTNRouter(t, adapter, false, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "recording not found")
}

func TestCvhPSTNWebhookAcceptsStatusCallback(t *testing.T) {
	adapter := &cvhPSTNAdapter{}
	r := cvhPSTNRouter(t, adapter, true, "https://pb.example.com")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1&CallStatus=completed", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String(), "状态回调只回空 200")
}

func TestCvhPSTNWebhookMediaEventNoError(t *testing.T) {
	adapter := &cvhPSTNAdapter{mediaEvents: []voiceprotocol.MediaEvent{{
		EventID:  "e3",
		Kind:     voiceprotocol.MediaEventRecordingStop,
		CallID:   "c1",
		Protocol: voiceprotocol.ProtocolHostedVendorWebhook,
	}}}
	r := cvhPSTNRouter(t, adapter, false, "")
	w := cvhPostPSTNRaw(t, r, "/public/voice/webhooks/cvh", "CallSid=c1", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}
