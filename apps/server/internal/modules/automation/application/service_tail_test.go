package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// timerFailRepo 注入 CreateTimer 失败，覆盖 scheduleTimer 的入队错误分支。
type timerFailRepo struct {
	stubRepo
	timerErr error
}

func (r *timerFailRepo) CreateTimer(ctx context.Context, timer *models.AutomationTimer) error {
	if r.timerErr != nil {
		return r.timerErr
	}
	return r.stubRepo.CreateTimer(ctx, timer)
}

func TestServiceSetTimerBatchSize(t *testing.T) {
	svc := NewService(&stubRepo{})
	if svc.timerBatchSize != 50 {
		t.Fatalf("default batch size = %d, want 50", svc.timerBatchSize)
	}
	svc.SetTimerBatchSize(7)
	if svc.timerBatchSize != 7 {
		t.Fatalf("batch size = %d, want 7", svc.timerBatchSize)
	}
	// 非正数保持原值
	svc.SetTimerBatchSize(0)
	if svc.timerBatchSize != 7 {
		t.Fatalf("batch size should stay 7, got %d", svc.timerBatchSize)
	}

	// 零值 Service（未走 NewService）时 ProcessDueTimers 回退默认 50
	bare := &Service{repo: &stubRepo{}}
	if n := bare.ProcessDueTimers(context.Background(), time.Now()); n != 0 {
		t.Fatalf("expected 0 processed, got %d", n)
	}
}

func TestBatchRunManualTriggerListError(t *testing.T) {
	svc := NewService(&stubRepo{listErr: errors.New("list down")})
	_, err := svc.BatchRun(context.Background(), BatchRunRequest{TriggerID: 3, TicketIDs: []uint{1}})
	if err == nil || err.Error() != "list down" {
		t.Fatalf("expected list error, got %v", err)
	}
}

func TestScheduleTimerMarshalAndCreateErrors(t *testing.T) {
	ctx := context.Background()
	delay := TriggerAction{
		Type: "delay",
		Params: map[string]interface{}{
			"minutes": 5,
			"actions": []TriggerAction{{
				Type:   "add_tag",
				Params: map[string]interface{}{"tag": map[string]interface{}{"boom": make(chan int)}},
			}},
		},
	}

	svc := NewService(&stubRepo{})
	// 嵌套动作里含不可序列化值 -> 快照 JSON 失败
	if _, err := svc.scheduleTimer(ctx, models.AutomationTrigger{ID: 1}, Event{TicketID: 1}, delay); err == nil || !strings.Contains(err.Error(), "invalid delay actions") {
		t.Fatalf("expected marshal error, got %v", err)
	}

	failRepo := &timerFailRepo{timerErr: errors.New("timer insert down")}
	svc = NewService(failRepo)
	sane := TriggerAction{
		Type: "delay",
		Params: map[string]interface{}{
			"minutes": 5,
			"actions": []TriggerAction{{Type: "add_tag", Params: map[string]interface{}{"tag": "vip"}}},
		},
	}
	if _, err := svc.scheduleTimer(ctx, models.AutomationTrigger{ID: 1}, Event{TicketID: 1}, sane); err == nil || err.Error() != "timer insert down" {
		t.Fatalf("expected create timer error, got %v", err)
	}
}

func TestProcessDueTimersRunFailures(t *testing.T) {
	ctx := context.Background()

	// ActionsJSON 非法 -> 记录 last_error 与 failed 审计
	repo := &stubRepo{dueTimers: []models.AutomationTimer{{
		ID: 1, TriggerID: 11, TicketID: 21, ActionsJSON: "{oops",
	}}}
	svc := NewService(repo)
	if n := svc.ProcessDueTimers(ctx, time.Now()); n != 1 {
		t.Fatalf("expected 1 processed, got %d", n)
	}
	if len(repo.lastErrors) != 1 || !strings.Contains(repo.lastErrors[0], "invalid delayed actions") {
		t.Fatalf("unexpected last errors: %v", repo.lastErrors)
	}
	if len(repo.runs) != 1 || repo.runs[0] != "failed" {
		t.Fatalf("unexpected runs: %v", repo.runs)
	}

	// 工单重载失败 -> 同样进入失败审计
	repo = &stubRepo{
		getTicketErr: errors.New("ticket gone"),
		dueTimers: []models.AutomationTimer{{
			ID: 2, TriggerID: 12, TicketID: 22,
			ActionsJSON: `[{"type":"add_tag","params":{"tag":"vip"}}]`,
		}},
	}
	svc = NewService(repo)
	if n := svc.ProcessDueTimers(ctx, time.Now()); n != 1 {
		t.Fatalf("expected 1 processed, got %d", n)
	}
	if len(repo.lastErrors) != 1 || !strings.Contains(repo.lastErrors[0], "ticket not found") {
		t.Fatalf("unexpected last errors: %v", repo.lastErrors)
	}
	if len(repo.runs) != 1 || repo.runs[0] != "failed" {
		t.Fatalf("unexpected runs: %v", repo.runs)
	}
}

func TestValidateDelayActionsDecodeError(t *testing.T) {
	actions := []TriggerAction{{
		Type:   "delay",
		Params: map[string]interface{}{"minutes": 5, "actions": "not-a-list"},
	}}
	err := validateDelayActions(actions, false)
	if err == nil || err.Error() != "delay requires actions param" {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestDelayMinutesParamTypes(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]interface{}
		want    int
		wantErr string
	}{
		{"float64 ok", map[string]interface{}{"minutes": float64(5)}, 5, ""},
		{"int ok", map[string]interface{}{"minutes": 7}, 7, ""},
		{"int non-positive", map[string]interface{}{"minutes": 0}, 0, "delay minutes must be positive"},
		{"non-number", map[string]interface{}{"minutes": "soon"}, 0, "delay minutes must be a number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := delayMinutes(tc.params)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("expected %d, got %d err %v", tc.want, got, err)
			}
		})
	}
}

func TestDecodeActionsInputVariants(t *testing.T) {
	in := []TriggerAction{{Type: "add_tag", Params: map[string]interface{}{"tag": "vip"}}}

	// Go 原生切片直接透传
	out, err := decodeActions(in)
	if err != nil || len(out) != 1 || out[0].Type != "add_tag" {
		t.Fatalf("unexpected passthrough: %+v err %v", out, err)
	}

	// JSON 反序列化产物 []interface{} 走归一化
	out, err = decodeActions([]interface{}{map[string]interface{}{"type": "notify_log"}})
	if err != nil || len(out) != 1 || out[0].Type != "notify_log" {
		t.Fatalf("unexpected normalized: %+v err %v", out, err)
	}

	// 不可序列化元素 -> marshal 失败
	if _, err := decodeActions([]interface{}{make(chan int)}); err == nil || !strings.Contains(err.Error(), "invalid delay actions") {
		t.Fatalf("expected marshal error, got %v", err)
	}

	// 元素形状不对 -> unmarshal 失败
	if _, err := decodeActions([]interface{}{"plain-string"}); err == nil || !strings.Contains(err.Error(), "invalid delay actions") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}

	// 非列表入参 -> 默认错误
	if _, err := decodeActions(42); err == nil || err.Error() != "delay requires actions param" {
		t.Fatalf("expected default error, got %v", err)
	}
}

func TestExecuteActionCallWebhookNonMapPayload(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	repo := &stubRepo{}
	svc := NewService(repo)
	svc.SetWebhookDispatcher(dispatcher)

	ticket := &TicketView{ID: 9, Priority: "normal", Status: "open"}
	err := svc.executeAction(context.Background(), TriggerAction{
		Type:   "call_webhook",
		Params: map[string]interface{}{"url": "https://example.com/hook", "payload": "plain text"},
	}, ticket)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dispatcher.url != "https://example.com/hook" {
		t.Fatalf("unexpected url: %s", dispatcher.url)
	}
	if dispatcher.payload["payload"] != "plain text" {
		t.Fatalf("expected raw payload passthrough, got %+v", dispatcher.payload)
	}
}
