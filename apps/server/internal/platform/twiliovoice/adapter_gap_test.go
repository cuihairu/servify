package twiliovoice

import (
	"context"
	"strings"
	"testing"
)

// TestFormFromPayloadShapes 覆盖三种合法 payload 形态的归一化。
func TestFormFromPayloadShapes(t *testing.T) {
	ctx := context.Background()
	adapter := NewAdapter(testToken)

	stringMap := map[string]string{
		"CallSid":    "CAmapstring",
		"CallStatus": "in-progress",
	}
	events, err := adapter.MapAnswer(ctx, stringMap)
	if err != nil {
		t.Fatalf("MapAnswer(map[string]string) error = %v", err)
	}
	if events.CallID != "CAmapstring" {
		t.Fatalf("CallID = %q, want CAmapstring", events.CallID)
	}

	sliceMap := map[string][]string{
		"CallSid":    {"CAmapslice"},
		"CallStatus": {"completed"},
	}
	event, err := adapter.MapHangup(ctx, sliceMap)
	if err != nil {
		t.Fatalf("MapHangup(map[string][]string) error = %v", err)
	}
	if event.CallID != "CAmapslice" || event.Kind != "hangup" {
		t.Fatalf("unexpected hangup event: %+v", event)
	}
}

// TestValidateSignatureMalformedBody 覆盖 form 解析失败分支（在签名校验前）。
func TestValidateSignatureMalformedBody(t *testing.T) {
	adapter := NewAdapter(testToken)
	err := adapter.ValidateSignature(context.Background(),
		"https://servify.example.com/public/voice/webhooks/twilio",
		map[string]string{webhookSignatureHeader: "whatever"},
		[]byte("invalid=%zz"))
	if err == nil || !strings.Contains(err.Error(), "parse form body") {
		t.Fatalf("malformed form body err = %v, want parse failure", err)
	}
}

// TestMapWebhookUnsupportedPayloadType 覆盖 MapWebhook 的 formFromPayload 错误分支。
func TestMapWebhookUnsupportedPayloadType(t *testing.T) {
	adapter := NewAdapter(testToken)
	_, _, err := adapter.MapWebhook(context.Background(), 12345)
	if err == nil || !strings.Contains(err.Error(), "unsupported webhook payload type") {
		t.Fatalf("MapWebhook(12345) err = %v, want unsupported type error", err)
	}
}

// TestCallEventCallDurationMetadata 覆盖 CallDuration 透传分支。
func TestCallEventCallDurationMetadata(t *testing.T) {
	adapter := NewAdapter(testToken)
	form := sampleForm()
	form.Set("CallDuration", "37")
	calls, _, err := adapter.MapWebhook(context.Background(), form)
	if err != nil {
		t.Fatalf("MapWebhook() error = %v", err)
	}
	if calls[0].Metadata["call_duration"] != "37" {
		t.Fatalf("call_duration metadata = %+v", calls[0].Metadata)
	}

	// 无 CallDuration 时不应出现该 key
	plain, _, err := adapter.MapWebhook(context.Background(), sampleForm())
	if err != nil {
		t.Fatalf("MapWebhook() error = %v", err)
	}
	if _, ok := plain[0].Metadata["call_duration"]; ok {
		t.Fatal("call_duration should be absent without the form field")
	}
}

// TestCallSignalingEntryPointsErrors 补齐各入口的错误分支。
func TestCallSignalingEntryPointsErrors(t *testing.T) {
	adapter := NewAdapter(testToken)
	ctx := context.Background()

	// MapInvite：状态不匹配
	inProgress := sampleForm()
	inProgress.Set("CallStatus", "in-progress")
	if _, err := adapter.MapInvite(ctx, inProgress); err == nil || !strings.Contains(err.Error(), "does not map to invite") {
		t.Fatalf("MapInvite(in-progress) err = %v", err)
	}

	// MapAnswer：payload 类型非法 / 状态不匹配
	if _, err := adapter.MapAnswer(ctx, 12345); err == nil || !strings.Contains(err.Error(), "unsupported webhook payload type") {
		t.Fatalf("MapAnswer(12345) err = %v", err)
	}
	if _, err := adapter.MapAnswer(ctx, sampleForm()); err == nil || !strings.Contains(err.Error(), "does not map to answer") {
		t.Fatalf("MapAnswer(queued) err = %v", err)
	}

	// MapHangup：payload 类型非法 / 状态不匹配
	if _, err := adapter.MapHangup(ctx, 12345); err == nil || !strings.Contains(err.Error(), "unsupported webhook payload type") {
		t.Fatalf("MapHangup(12345) err = %v", err)
	}
	if _, err := adapter.MapHangup(ctx, sampleForm()); err == nil || !strings.Contains(err.Error(), "does not map to hangup") {
		t.Fatalf("MapHangup(queued) err = %v", err)
	}

	// MapDTMF：payload 类型非法 / 缺 Digits
	if _, err := adapter.MapDTMF(ctx, 12345); err == nil || !strings.Contains(err.Error(), "unsupported webhook payload type") {
		t.Fatalf("MapDTMF(12345) err = %v", err)
	}
	if _, err := adapter.MapDTMF(ctx, sampleForm()); err == nil || !strings.Contains(err.Error(), "missing Digits") {
		t.Fatalf("MapDTMF(no digits) err = %v", err)
	}
	event, err := adapter.MapDTMF(ctx, map[string]string{"CallSid": "CAx", "Digits": "5"})
	if err != nil {
		t.Fatalf("MapDTMF() error = %v", err)
	}
	if event.Metadata["digits"] != "5" {
		t.Fatalf("unexpected DTMF event: %+v", event)
	}
}
