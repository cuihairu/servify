package twiliovoice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/platform/voiceprotocol"
)

const testToken = "test-auth-token"

func twilioSignature(token, requestURL string, form url.Values) string {
	mac := hmac.New(sha1.New, []byte(token))
	mac.Write(signedMaterial(requestURL, form))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func sampleForm() url.Values {
	return url.Values{
		"CallSid":    {"CAabcdef1234567890"},
		"CallStatus": {"queued"},
		"From":       {"+15550001111"},
		"To":         {"+15550002222"},
		"Timestamp":  {"Fri, 11 Sep 2026 08:30:00 +0000"},
	}
}

func TestAdapterImplementsHostedVendorContract(t *testing.T) {
	adapter := NewAdapter(testToken)
	if adapter.Name() != "twilio-voice" {
		t.Fatalf("Name() = %q", adapter.Name())
	}
	if adapter.Protocol() != voiceprotocol.ProtocolHostedVendorWebhook {
		t.Fatalf("Protocol() = %q", adapter.Protocol())
	}
	// 固定 webhook path:路径是签名材料的一部分,必须终生稳定。
	if got := adapter.WebhookPath(); got != "/public/voice/webhooks/twilio" {
		t.Fatalf("WebhookPath() = %q, want /public/voice/webhooks/twilio", got)
	}
}

func TestValidateSignature(t *testing.T) {
	adapter := NewAdapter(testToken)
	ctx := context.Background()
	requestURL := "https://servify.example.com/public/voice/webhooks/twilio"
	form := sampleForm()
	body := []byte(form.Encode())

	if err := adapter.ValidateSignature(ctx, requestURL, map[string]string{"X-Twilio-Signature": twilioSignature(testToken, requestURL, form)}, body); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	// 篡改 body(改一个参数值;纯追加 & 不改变解析结果,签名本应通过)
	tampered := strings.Replace(string(body), "CAabcdef1234567890", "CAffffff9999999999", 1)
	if tampered == string(body) {
		t.Fatal("tamper target not found in body")
	}
	if err := adapter.ValidateSignature(ctx, requestURL, map[string]string{"X-Twilio-Signature": twilioSignature(testToken, requestURL, form)}, []byte(tampered)); err == nil {
		t.Fatal("tampered body accepted")
	}
	// 错误 token
	if err := adapter.ValidateSignature(ctx, requestURL, map[string]string{"X-Twilio-Signature": twilioSignature("wrong-token", requestURL, form)}, body); err == nil {
		t.Fatal("wrong-token signature accepted")
	}
	// 缺 header
	if err := adapter.ValidateSignature(ctx, requestURL, map[string]string{}, body); err == nil {
		t.Fatal("missing signature header accepted")
	}
	// 未配置 token
	if err := NewAdapter("").ValidateSignature(ctx, requestURL, map[string]string{"X-Twilio-Signature": twilioSignature(testToken, requestURL, form)}, body); err == nil {
		t.Fatal("unconfigured token accepted")
	}
	// URL 变化(签名材料含完整 URL)
	if err := adapter.ValidateSignature(ctx, "https://evil.example.com/public/voice/webhooks/twilio", map[string]string{"X-Twilio-Signature": twilioSignature(testToken, requestURL, form)}, body); err == nil {
		t.Fatal("signature bound to a different URL accepted")
	}
}

func TestMapWebhookCallStatuses(t *testing.T) {
	adapter := NewAdapter(testToken)
	ctx := context.Background()

	cases := []struct {
		status string
		kind   voiceprotocol.CallEventKind
	}{
		{"queued", voiceprotocol.CallEventInvite},
		{"ringing", voiceprotocol.CallEventInvite},
		{"in-progress", voiceprotocol.CallEventAnswer},
		{"completed", voiceprotocol.CallEventHangup},
		{"busy", voiceprotocol.CallEventHangup},
		{"no-answer", voiceprotocol.CallEventHangup},
		{"failed", voiceprotocol.CallEventHangup},
		{"canceled", voiceprotocol.CallEventHangup},
	}
	for _, tc := range cases {
		form := sampleForm()
		form.Set("CallStatus", tc.status)
		calls, media, err := adapter.MapWebhook(ctx, form)
		if err != nil {
			t.Fatalf("MapWebhook(%s) error = %v", tc.status, err)
		}
		if len(media) != 0 {
			t.Fatalf("MapWebhook(%s) produced media events: %+v", tc.status, media)
		}
		if len(calls) != 1 || calls[0].Kind != tc.kind {
			t.Fatalf("MapWebhook(%s) = %+v, want kind %s", tc.status, calls, tc.kind)
		}
	}
}

func TestMapWebhookEventShape(t *testing.T) {
	adapter := NewAdapter(testToken)
	calls, _, err := adapter.MapWebhook(context.Background(), sampleForm())
	if err != nil {
		t.Fatalf("MapWebhook() error = %v", err)
	}
	event := calls[0]
	if event.EventID != "twilio-invite-CAabcdef1234567890" {
		t.Fatalf("EventID = %q (want deterministic per kind+CallSid)", event.EventID)
	}
	if event.CallID != "CAabcdef1234567890" || event.From != "+15550001111" || event.To != "+15550002222" {
		t.Fatalf("unexpected event fields: %+v", event)
	}
	// Timestamp(RFC1123Z)必须被解析而非回退到 now。
	want, _ := time.Parse(time.RFC1123Z, "Fri, 11 Sep 2026 08:30:00 +0000")
	if !event.OccurredAt.Equal(want) {
		t.Fatalf("OccurredAt = %v, want %v", event.OccurredAt, want)
	}
	if event.Metadata["provider"] != "twilio" || event.Metadata["call_status"] != "queued" {
		t.Fatalf("unexpected metadata: %+v", event.Metadata)
	}
}

func TestMapWebhookDTMFAppended(t *testing.T) {
	adapter := NewAdapter(testToken)
	form := sampleForm()
	form.Set("Digits", "1234")
	calls, _, err := adapter.MapWebhook(context.Background(), form)
	if err != nil {
		t.Fatalf("MapWebhook() error = %v", err)
	}
	if len(calls) != 2 || calls[1].Kind != voiceprotocol.CallEventDTMF {
		t.Fatalf("expected DTMF event appended, got %+v", calls)
	}
	if calls[1].Metadata["digits"] != "1234" {
		t.Fatalf("digits metadata = %+v", calls[1].Metadata)
	}
}

func TestMapWebhookRecordingPriority(t *testing.T) {
	adapter := NewAdapter(testToken)
	form := url.Values{
		"CallSid":           {"CAabcdef1234567890"},
		"RecordingSid":      {"REabcdef1234567890"},
		"RecordingUrl":      {"https://api.twilio.com/recordings/REabcdef1234567890.mp3"},
		"RecordingStatus":   {"completed"},
		"RecordingDuration": {"42"},
	}
	calls, media, err := adapter.MapWebhook(context.Background(), form)
	if err != nil {
		t.Fatalf("MapWebhook() error = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("recording callback must not emit call events: %+v", calls)
	}
	if len(media) != 1 {
		t.Fatalf("expected one media event, got %+v", media)
	}
	event := media[0]
	if event.Kind != voiceprotocol.MediaEventRecordingStop {
		t.Fatalf("Kind = %q", event.Kind)
	}
	if event.EventID != "twilio-recording-REabcdef1234567890" {
		t.Fatalf("EventID = %q", event.EventID)
	}
	if event.CallID != "CAabcdef1234567890" {
		t.Fatalf("CallID = %q", event.CallID)
	}
	if event.Metadata["storage_uri"] != form.Get("RecordingUrl") || event.Metadata["recording_id"] != form.Get("RecordingSid") {
		t.Fatalf("unexpected metadata: %+v", event.Metadata)
	}
}

func TestMapWebhookErrors(t *testing.T) {
	adapter := NewAdapter(testToken)
	ctx := context.Background()

	// 未知 CallStatus
	bad := sampleForm()
	bad.Set("CallStatus", "teleported")
	if _, _, err := adapter.MapWebhook(ctx, bad); err == nil || !strings.Contains(err.Error(), "unsupported CallStatus") {
		t.Fatalf("unknown status err = %v", err)
	}
	// 缺 CallSid
	missing := url.Values{"CallStatus": {"queued"}}
	if _, _, err := adapter.MapWebhook(ctx, missing); err == nil || !strings.Contains(err.Error(), "CallSid") {
		t.Fatalf("missing CallSid err = %v", err)
	}
	// Timestamp 损坏回退 now(不报错)
	brokenTS := sampleForm()
	brokenTS.Set("Timestamp", "not-a-timestamp")
	calls, _, err := adapter.MapWebhook(ctx, brokenTS)
	if err != nil {
		t.Fatalf("broken timestamp err = %v", err)
	}
	if calls[0].OccurredAt.IsZero() {
		t.Fatal("OccurredAt must fall back to non-zero time")
	}
}

func TestCallSignalingEntryPoints(t *testing.T) {
	adapter := NewAdapter(testToken)
	ctx := context.Background()

	if _, err := adapter.MapInvite(ctx, sampleForm()); err != nil {
		t.Fatalf("MapInvite error = %v", err)
	}
	inProgress := sampleForm()
	inProgress.Set("CallStatus", "in-progress")
	if _, err := adapter.MapAnswer(ctx, inProgress); err != nil {
		t.Fatalf("MapAnswer error = %v", err)
	}
	done := sampleForm()
	done.Set("CallStatus", "completed")
	if _, err := adapter.MapHangup(ctx, done); err != nil {
		t.Fatalf("MapHangup error = %v", err)
	}
	digits := sampleForm()
	digits.Set("Digits", "9")
	if _, err := adapter.MapDTMF(ctx, digits); err != nil {
		t.Fatalf("MapDTMF error = %v", err)
	}

	// 状态不匹配的入口调用要报错
	if _, err := adapter.MapAnswer(ctx, sampleForm()); err == nil {
		t.Fatal("MapAnswer with queued status should error")
	}
	// hold/resume/transfer 在 Twilio status callback 上不可表达
	for name, fn := range map[string]func() error{
		"MapHold":     func() error { _, err := adapter.MapHold(ctx, sampleForm()); return err },
		"MapResume":   func() error { _, err := adapter.MapResume(ctx, sampleForm()); return err },
		"MapTransfer": func() error { _, err := adapter.MapTransfer(ctx, sampleForm()); return err },
	} {
		if err := fn(); err == nil || !strings.Contains(err.Error(), "do not carry") {
			t.Fatalf("%s err = %v, want unsupported-state error", name, err)
		}
	}
	// 非法 payload 类型
	if _, err := adapter.MapInvite(ctx, 12345); err == nil {
		t.Fatal("unsupported payload type should error")
	}
}
