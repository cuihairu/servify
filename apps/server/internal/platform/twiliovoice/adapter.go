package twiliovoice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"servify/apps/server/internal/platform/voiceprotocol"
)

// compile-time contract checks.
var _ voiceprotocol.HostedVendorWebhookAdapter = (*Adapter)(nil)

// Adapter maps Twilio Voice status callbacks into normalized voice protocol
// events. It validates X-Twilio-Signature (HMAC-SHA1 over the full request
// URL plus sorted form parameters, keyed by the auth token) before any
// payload is trusted.
type Adapter struct {
	authToken string
}

// NewAdapter builds the adapter; authToken must be the Twilio auth token
// shared with the configured account (reuses voice.twilio.auth_token).
func NewAdapter(authToken string) *Adapter {
	return &Adapter{authToken: authToken}
}

func (a *Adapter) Name() string {
	return "twilio-voice"
}

func (a *Adapter) Protocol() voiceprotocol.Protocol {
	return voiceprotocol.ProtocolHostedVendorWebhook
}

// WebhookPath is fixed: the path is part of the signed URL and must stay
// stable for the lifetime of the integration.
func (a *Adapter) WebhookPath() string {
	return "/public/voice/webhooks/twilio"
}

// ValidateSignature verifies X-Twilio-Signature in constant time.
// requestURL must be the exact URL Twilio signed: public base URL + request
// URI in production (assembled by the HTTP layer), request host + URI locally.
func (a *Adapter) ValidateSignature(ctx context.Context, requestURL string, headers map[string]string, body []byte) error {
	if a.authToken == "" {
		return fmt.Errorf("twilio auth token is not configured")
	}
	signature := headers[webhookSignatureHeader]
	if signature == "" {
		return fmt.Errorf("missing %s header", webhookSignatureHeader)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("parse form body: %w", err)
	}
	mac := hmac.New(sha1.New, []byte(a.authToken))
	mac.Write(signedMaterial(requestURL, form))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// MapWebhook maps one status callback into normalized events. Recording
// status callbacks take priority (they may arrive without CallStatus);
// otherwise the call status decides the signaling kind, and DTMF digits
// (gather results) are appended as a separate event.
func (a *Adapter) MapWebhook(ctx context.Context, form url.Values) ([]voiceprotocol.CallEvent, []voiceprotocol.MediaEvent, error) {
	if form.Get("RecordingSid") != "" && (form.Get("RecordingUrl") != "" || form.Get("RecordingStatus") == "completed") {
		return nil, []voiceprotocol.MediaEvent{a.recordingEvent(form)}, nil
	}
	if form.Get("CallSid") == "" {
		return nil, nil, fmt.Errorf("webhook payload missing CallSid")
	}
	var callEvents []voiceprotocol.CallEvent
	switch form.Get("CallStatus") {
	case "queued", "ringing":
		callEvents = append(callEvents, a.callEvent(form, voiceprotocol.CallEventInvite))
	case "in-progress":
		callEvents = append(callEvents, a.callEvent(form, voiceprotocol.CallEventAnswer))
	case "completed", "busy", "no-answer", "failed", "canceled":
		callEvents = append(callEvents, a.callEvent(form, voiceprotocol.CallEventHangup))
	default:
		return nil, nil, fmt.Errorf("unsupported CallStatus %q", form.Get("CallStatus"))
	}
	if digits := form.Get("Digits"); digits != "" {
		callEvents = append(callEvents, a.dtmfEvent(form))
	}
	return callEvents, nil, nil
}

// callEvent builds the normalized call event; EventID is deterministic per
// (kind, CallSid) so duplicate deliveries collide visibly.
func (a *Adapter) callEvent(form url.Values, kind voiceprotocol.CallEventKind) voiceprotocol.CallEvent {
	metadata := map[string]interface{}{
		"provider":    "twilio",
		"event_type":  "CallStatus",
		"call_status": form.Get("CallStatus"),
	}
	if duration := form.Get("CallDuration"); duration != "" {
		metadata["call_duration"] = duration
	}
	return voiceprotocol.CallEvent{
		EventID:    fmt.Sprintf("twilio-%s-%s", kind, form.Get("CallSid")),
		Protocol:   voiceprotocol.ProtocolHostedVendorWebhook,
		Kind:       kind,
		CallID:     form.Get("CallSid"),
		From:       form.Get("From"),
		To:         form.Get("To"),
		OccurredAt: occurredAt(form),
		Metadata:   metadata,
	}
}

func (a *Adapter) dtmfEvent(form url.Values) voiceprotocol.CallEvent {
	event := a.callEvent(form, voiceprotocol.CallEventDTMF)
	event.Metadata["digits"] = form.Get("Digits")
	delete(event.Metadata, "call_status")
	return event
}

// recordingEvent maps a Recording Status Callback into a recording_stopped
// media event carrying the storage URI; the coordinator upserts it as the
// recording terminal state.
func (a *Adapter) recordingEvent(form url.Values) voiceprotocol.MediaEvent {
	metadata := map[string]interface{}{
		"provider":         "twilio",
		"recording_id":     form.Get("RecordingSid"),
		"storage_uri":      form.Get("RecordingUrl"),
		"recording_status": form.Get("RecordingStatus"),
	}
	if duration := form.Get("RecordingDuration"); duration != "" {
		metadata["duration"] = duration
	}
	return voiceprotocol.MediaEvent{
		EventID:    "twilio-recording-" + form.Get("RecordingSid"),
		Protocol:   voiceprotocol.ProtocolHostedVendorWebhook,
		Kind:       voiceprotocol.MediaEventRecordingStop,
		CallID:     form.Get("CallSid"),
		OccurredAt: occurredAt(form),
		Metadata:   metadata,
	}
}

// occurredAt parses Twilio's RFC1123Z Timestamp; malformed or missing values
// fall back to server time rather than dropping the event.
func occurredAt(form url.Values) time.Time {
	if raw := form.Get("Timestamp"); raw != "" {
		if parsed, err := time.Parse(time.RFC1123Z, raw); err == nil {
			return parsed
		}
	}
	return time.Now()
}

// --- CallSignalingAdapter (generic registry entry points) ---

func (a *Adapter) MapInvite(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	form, err := formFromPayload(payload)
	if err != nil {
		return voiceprotocol.CallEvent{}, err
	}
	switch form.Get("CallStatus") {
	case "queued", "ringing":
		return a.callEvent(form, voiceprotocol.CallEventInvite), nil
	default:
		return voiceprotocol.CallEvent{}, fmt.Errorf("CallStatus %q does not map to invite", form.Get("CallStatus"))
	}
}

func (a *Adapter) MapAnswer(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	form, err := formFromPayload(payload)
	if err != nil {
		return voiceprotocol.CallEvent{}, err
	}
	if form.Get("CallStatus") != "in-progress" {
		return voiceprotocol.CallEvent{}, fmt.Errorf("CallStatus %q does not map to answer", form.Get("CallStatus"))
	}
	return a.callEvent(form, voiceprotocol.CallEventAnswer), nil
}

func (a *Adapter) MapHangup(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	form, err := formFromPayload(payload)
	if err != nil {
		return voiceprotocol.CallEvent{}, err
	}
	switch form.Get("CallStatus") {
	case "completed", "busy", "no-answer", "failed", "canceled":
		return a.callEvent(form, voiceprotocol.CallEventHangup), nil
	default:
		return voiceprotocol.CallEvent{}, fmt.Errorf("CallStatus %q does not map to hangup", form.Get("CallStatus"))
	}
}

// Twilio Call Status Callbacks carry no hold/resume/transfer state; these
// lifecycle transitions are not representable on this provider yet.
func (a *Adapter) MapHold(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, fmt.Errorf("twilio status callbacks do not carry hold state")
}

func (a *Adapter) MapResume(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, fmt.Errorf("twilio status callbacks do not carry resume state")
}

func (a *Adapter) MapTransfer(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	return voiceprotocol.CallEvent{}, fmt.Errorf("twilio status callbacks do not carry transfer state")
}

func (a *Adapter) MapDTMF(ctx context.Context, payload interface{}) (voiceprotocol.CallEvent, error) {
	form, err := formFromPayload(payload)
	if err != nil {
		return voiceprotocol.CallEvent{}, err
	}
	if form.Get("Digits") == "" {
		return voiceprotocol.CallEvent{}, fmt.Errorf("webhook payload missing Digits")
	}
	return a.dtmfEvent(form), nil
}
