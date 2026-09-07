package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	voiceapp "servify/apps/server/internal/modules/voice/application"
	voicedelivery "servify/apps/server/internal/modules/voice/delivery"
	voiceprovidermock "servify/apps/server/internal/modules/voice/provider/mock"
	"servify/apps/server/internal/platform/voiceprotocol"

	"github.com/gin-gonic/gin"
)

type cxcTranscriptRepo struct {
	listErr  error
	listAll  error
	appended int
}

func (r *cxcTranscriptRepo) Append(ctx context.Context, t voiceapp.TranscriptDTO) error {
	r.appended++
	return nil
}
func (r *cxcTranscriptRepo) ListByCallID(ctx context.Context, callID string) ([]voiceapp.TranscriptDTO, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return nil, nil
}
func (r *cxcTranscriptRepo) ListAll(ctx context.Context, page, pageSize int) ([]voiceapp.TranscriptDTO, int64, error) {
	if r.listAll != nil {
		return nil, 0, r.listAll
	}
	return nil, 0, nil
}

func TestCxcVoiceRecordingLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewVoiceHandler(newVoiceCoordinatorForTest(), newVoiceRegistryForTest())
	r := gin.New()
	r.POST("/voice/recordings/start", handler.StartRecording)
	r.POST("/voice/recordings/stop", handler.StopRecording)
	r.GET("/voice/recordings/:recordingID", handler.GetRecording)
	r.GET("/voice/recordings", handler.GetRecording)

	// start
	w := cxcPerform(r, http.MethodPost, "/voice/recordings/start", cxcJSONBody(map[string]string{"call_id": "call-1"}), "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("start expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var startResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("unmarshal start: %v", err)
	}
	if startResp.Data.ID == "" {
		t.Fatalf("expected recording id, got %s", w.Body.String())
	}

	// get success
	w = cxcPerform(r, http.MethodGet, "/voice/recordings/"+startResp.Data.ID, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// get missing -> 500
	w = cxcPerform(r, http.MethodGet, "/voice/recordings/rec-missing", nil, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("get missing expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	// get without :recordingID param -> 400
	w = cxcPerform(r, http.MethodGet, "/voice/recordings", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("get without param expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// stop success
	w = cxcPerform(r, http.MethodPost, "/voice/recordings/stop", cxcJSONBody(map[string]string{"recording_id": startResp.Data.ID}), "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("stop expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// stop missing -> repo MarkStopped fails -> 500
	w = cxcPerform(r, http.MethodPost, "/voice/recordings/stop", cxcJSONBody(map[string]string{"recording_id": "rec-missing"}), "application/json")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("stop missing expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCxcVoiceHandlerRuntimeUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	noCoord := NewVoiceHandler(nil, newVoiceRegistryForTest())
	r := gin.New()
	r.POST("/voice/recordings/start", noCoord.StartRecording)
	r.POST("/voice/recordings/stop", noCoord.StopRecording)
	r.GET("/voice/recordings/:recordingID", noCoord.GetRecording)
	r.POST("/voice/transcripts", noCoord.AppendTranscript)
	r.GET("/voice/transcripts", noCoord.ListTranscripts)
	r.POST("/voice/protocols/:protocol/call-events/:event", noCoord.HandleProtocolCallEvent)
	r.POST("/voice/protocols/:protocol/media-events/:event", noCoord.HandleProtocolMediaEvent)

	for _, path := range []string{"/voice/recordings/start", "/voice/recordings/stop", "/voice/transcripts"} {
		if w := cxcPerform(r, http.MethodPost, path, cxcJSONBody(map[string]string{}), "application/json"); w.Code != http.StatusNotFound {
			t.Fatalf("%s expected 404, got %d", path, w.Code)
		}
	}
	for _, path := range []string{"/voice/recordings/rec-1", "/voice/transcripts"} {
		if w := cxcPerform(r, http.MethodGet, path, nil, ""); w.Code != http.StatusNotFound {
			t.Fatalf("%s expected 404, got %d", path, w.Code)
		}
	}

	// registry nil: protocols listing and protocol events
	noReg := NewVoiceHandler(newVoiceCoordinatorForTest(), nil)
	r2 := gin.New()
	r2.GET("/voice/protocols", noReg.ListProtocols)
	r2.POST("/voice/protocols/:protocol/call-events/:event", noReg.HandleProtocolCallEvent)
	r2.POST("/voice/protocols/:protocol/media-events/:event", noReg.HandleProtocolMediaEvent)
	if w := cxcPerform(r2, http.MethodGet, "/voice/protocols", nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("protocols expected 404, got %d", w.Code)
	}
	if w := cxcPerform(r2, http.MethodPost, "/voice/protocols/sip/call-events/invite", cxcJSONBody(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json"); w.Code != http.StatusNotFound {
		t.Fatalf("call event expected 404, got %d", w.Code)
	}
	if w := cxcPerform(r2, http.MethodPost, "/voice/protocols/rtp/media-events/session_started", cxcJSONBody(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json"); w.Code != http.StatusNotFound {
		t.Fatalf("media event expected 404, got %d", w.Code)
	}

	// both nil
	neither := NewVoiceHandler(nil, nil)
	r3 := gin.New()
	r3.POST("/voice/protocols/:protocol/call-events/:event", neither.HandleProtocolCallEvent)
	r3.POST("/voice/protocols/:protocol/media-events/:event", neither.HandleProtocolMediaEvent)
	if w := cxcPerform(r3, http.MethodPost, "/voice/protocols/sip/call-events/invite", cxcJSONBody(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json"); w.Code != http.StatusNotFound {
		t.Fatalf("call event (nil runtime) expected 404, got %d", w.Code)
	}
	if w := cxcPerform(r3, http.MethodPost, "/voice/protocols/rtp/media-events/session_started", cxcJSONBody(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json"); w.Code != http.StatusNotFound {
		t.Fatalf("media event (nil runtime) expected 404, got %d", w.Code)
	}
}

func TestCxcVoiceHandlerBadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewVoiceHandler(newVoiceCoordinatorForTest(), newVoiceRegistryForTest())
	r := gin.New()
	r.POST("/voice/recordings/start", handler.StartRecording)
	r.POST("/voice/recordings/stop", handler.StopRecording)
	r.POST("/voice/transcripts", handler.AppendTranscript)
	r.POST("/voice/protocols/:protocol/call-events/:event", handler.HandleProtocolCallEvent)
	r.POST("/voice/protocols/:protocol/media-events/:event", handler.HandleProtocolMediaEvent)

	paths := []string{
		"/voice/recordings/start",
		"/voice/recordings/stop",
		"/voice/transcripts",
		"/voice/protocols/sip/call-events/invite",
		"/voice/protocols/rtp/media-events/session_started",
	}
	for _, path := range paths {
		w := cxcPerform(r, http.MethodPost, path, strings.NewReader("{bad"), "application/json")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s expected 400, got %d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestCxcVoiceListTranscriptsAllAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewVoiceHandler(newVoiceCoordinatorForTest(), newVoiceRegistryForTest())
	r := gin.New()
	r.GET("/voice/transcripts", handler.ListTranscripts)

	w := cxcPerform(r, http.MethodGet, "/voice/transcripts?page=2&page_size=5", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Page != 2 || resp.PageSize != 5 {
		t.Fatalf("expected page=2 page_size=5, got %+v", resp)
	}

	// invalid pagination falls back to defaults
	w = cxcPerform(r, http.MethodGet, "/voice/transcripts?page=abc&page_size=0", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp2 struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Page != 1 || resp2.PageSize != 10 {
		t.Fatalf("expected defaults page=1 page_size=10, got %+v", resp2)
	}
}

func TestCxcVoiceListTranscriptsErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &cxcTranscriptRepo{}
	ts := voiceapp.NewTranscriptService(voiceprovidermock.NewTranscriptProvider(), repo, &voiceTestBus{})
	coord := voicedelivery.NewCoordinator(nil, nil, ts)
	handler := NewVoiceHandler(coord, newVoiceRegistryForTest())
	r := gin.New()
	r.GET("/voice/transcripts", handler.ListTranscripts)

	repo.listErr = errors.New("list failed")
	if w := cxcPerform(r, http.MethodGet, "/voice/transcripts?call_id=c1", nil, ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("call_id list expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	repo.listErr = nil
	repo.listAll = errors.New("list all failed")
	if w := cxcPerform(r, http.MethodGet, "/voice/transcripts", nil, ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("list all expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCxcVoiceProtocolEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewVoiceHandler(newVoiceCoordinatorForTest(), newVoiceRegistryForTest())
	r := gin.New()
	r.POST("/voice/protocols/:protocol/call-events/:event", handler.HandleProtocolCallEvent)
	r.POST("/voice/protocols/:protocol/media-events/:event", handler.HandleProtocolMediaEvent)

	payload := cxcJSONBody

	// unsupported signaling protocol
	w := cxcPerform(r, http.MethodPost, "/voice/protocols/unknown/call-events/invite", payload(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unsupported call protocol expected 404, got %d body=%s", w.Code, w.Body.String())
	}

	// unsupported event name
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/sip/call-events/bogus", payload(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown call event expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// mapped ok but coordinator fails (answer for missing call) -> 500
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/sip/call-events/answer", payload(map[string]interface{}{"payload": map[string]interface{}{"call_id": "missing"}}), "application/json")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("answer missing call expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	// unsupported media protocol
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/sip/media-events/session_started", payload(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unsupported media protocol expected 404, got %d body=%s", w.Code, w.Body.String())
	}

	// unsupported media event name
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/rtp/media-events/bogus", payload(map[string]interface{}{"payload": map[string]interface{}{}}), "application/json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown media event expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// media success (session_started -> no-op)
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/rtp/media-events/session_started", payload(map[string]interface{}{"payload": map[string]interface{}{"call_id": "c1"}}), "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("media session_started expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// media recording started -> mock provider succeeds
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/rtp/media-events/recording_started", payload(map[string]interface{}{"payload": map[string]interface{}{"call_id": "c1"}}), "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("media recording_started expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// media recording stopped with unknown recording id -> 500
	w = cxcPerform(r, http.MethodPost, "/voice/protocols/rtp/media-events/recording_stopped", payload(map[string]interface{}{"payload": map[string]interface{}{"call_id": "c1", "metadata": map[string]interface{}{"recording_id": "rec-missing"}}}), "application/json")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("media recording_stopped expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCxcRegisterVoiceRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// nil arguments return early without panicking
	RegisterVoiceRoutes(nil, nil)

	handler := NewVoiceHandler(newVoiceCoordinatorForTest(), newVoiceRegistryForTest())
	r := gin.New()
	api := r.Group("/api")
	RegisterVoiceRoutes(api, handler)
	RegisterVoiceRoutes(api, nil)

	w := cxcPerform(r, http.MethodGet, "/api/voice/protocols?page=2&page_size=1", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("protocols expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	w = cxcPerform(r, http.MethodGet, "/api/voice/protocols?page=bad&page_size=bad", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("protocols with bad paging expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

type cxcSignalingAdapter struct{ lastKind string }

func (a *cxcSignalingAdapter) Name() string                     { return "cxc-sip" }
func (a *cxcSignalingAdapter) Protocol() voiceprotocol.Protocol { return voiceprotocol.ProtocolSIP }
func (a *cxcSignalingAdapter) MapInvite(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "invite"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventInvite}, nil
}
func (a *cxcSignalingAdapter) MapAnswer(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "answer"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventAnswer}, nil
}
func (a *cxcSignalingAdapter) MapHold(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "hold"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHold}, nil
}
func (a *cxcSignalingAdapter) MapResume(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "resume"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventResume}, nil
}
func (a *cxcSignalingAdapter) MapHangup(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "hangup"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventHangup}, nil
}
func (a *cxcSignalingAdapter) MapTransfer(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "transfer"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventTransfer}, nil
}
func (a *cxcSignalingAdapter) MapDTMF(ctx context.Context, p interface{}) (voiceprotocol.CallEvent, error) {
	a.lastKind = "dtmf"
	return voiceprotocol.CallEvent{Kind: voiceprotocol.CallEventDTMF}, nil
}

type cxcMediaAdapter struct{ lastKind string }

func (a *cxcMediaAdapter) Name() string                     { return "cxc-rtp" }
func (a *cxcMediaAdapter) Protocol() voiceprotocol.Protocol { return voiceprotocol.ProtocolRTP }
func (a *cxcMediaAdapter) MapSessionStarted(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "session_started"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventSessionStarted}, nil
}
func (a *cxcMediaAdapter) MapSessionClosed(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "session_closed"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventSessionClosed}, nil
}
func (a *cxcMediaAdapter) MapTrackMuted(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "track_muted"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventTrackMuted}, nil
}
func (a *cxcMediaAdapter) MapTrackUnmuted(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "track_unmuted"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventTrackUnmuted}, nil
}
func (a *cxcMediaAdapter) MapRecordingStarted(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "recording_started"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventRecordingStart}, nil
}
func (a *cxcMediaAdapter) MapRecordingStopped(ctx context.Context, p interface{}) (voiceprotocol.MediaEvent, error) {
	a.lastKind = "recording_stopped"
	return voiceprotocol.MediaEvent{Kind: voiceprotocol.MediaEventRecordingStop}, nil
}

func TestCxcMapCallEventDispatch(t *testing.T) {
	adapter := &cxcSignalingAdapter{}
	ctx := context.Background()
	for _, kind := range []string{"invite", "answer", "hold", "resume", "hangup", "transfer", "dtmf"} {
		ev, err := mapCallEvent(ctx, adapter, kind, nil)
		if err != nil {
			t.Fatalf("mapCallEvent(%q) error: %v", kind, err)
		}
		if adapter.lastKind != kind {
			t.Fatalf("mapCallEvent(%q) dispatched to %q", kind, adapter.lastKind)
		}
		if string(ev.Kind) != kind {
			t.Fatalf("unexpected event kind %q", ev.Kind)
		}
	}
	if _, err := mapCallEvent(ctx, adapter, "bogus", nil); err == nil {
		t.Fatal("mapCallEvent(bogus) expected error")
	}
}

func TestCxcMapMediaEventDispatch(t *testing.T) {
	adapter := &cxcMediaAdapter{}
	ctx := context.Background()
	for _, kind := range []string{"session_started", "session_closed", "track_muted", "track_unmuted", "recording_started", "recording_stopped"} {
		ev, err := mapMediaEvent(ctx, adapter, kind, nil)
		if err != nil {
			t.Fatalf("mapMediaEvent(%q) error: %v", kind, err)
		}
		if adapter.lastKind != kind {
			t.Fatalf("mapMediaEvent(%q) dispatched to %q", kind, adapter.lastKind)
		}
		if string(ev.Kind) != kind {
			t.Fatalf("unexpected event kind %q", ev.Kind)
		}
	}
	if _, err := mapMediaEvent(ctx, adapter, "bogus", nil); err == nil {
		t.Fatal("mapMediaEvent(bogus) expected error")
	}
}
