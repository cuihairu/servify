package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/modules/conversation/domain"

	"github.com/gin-gonic/gin"
)

type scriptedTimelineRepo struct {
	fakeEventRepo
	events    []domain.ConversationEvent
	gotLimit  int
	gotOffset int
}

func (f *scriptedTimelineRepo) ListByConversation(_ context.Context, _ string, limit, offset int) ([]domain.ConversationEvent, error) {
	f.gotLimit = limit
	f.gotOffset = offset
	return f.events, nil
}

func newTimelineTestContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	ctx.Params = gin.Params{{Key: "id", Value: "conv-1"}}
	return ctx, recorder
}

func TestTimelineAdapter_ReturnsChronologicalTimeline(t *testing.T) {
	now := time.Now()
	// 仓储口径：新→旧返回（与 GORM Order occurred_at DESC 一致）。
	repo := &scriptedTimelineRepo{events: []domain.ConversationEvent{
		{EventType: "ticket.created", ActorType: "system", Summary: "创建工单", OccurredAt: now},
		{EventType: "routing.agent_assigned", ActorType: "routing", Summary: "分配坐席", OccurredAt: now.Add(-time.Minute)},
		{EventType: "conversation.created", ActorType: "system", Summary: "会话创建", OccurredAt: now.Add(-2 * time.Minute)},
	}}
	adapter := NewTimelineAdapter(repo)
	ctx, recorder := newTimelineTestContext(t, "/api/v1/workspace/omni/sessions/conv-1/timeline")

	adapter.HandleListTimeline(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response TimelineResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response should be JSON: %v", err)
	}
	if response.ConversationID != "conv-1" || response.Count != 3 {
		t.Fatalf("unexpected response: %+v", response)
	}
	// 旧→新时序（展示即阅读顺序）。
	if response.Items[0].EventType != "conversation.created" || response.Items[2].EventType != "ticket.created" {
		t.Errorf("timeline should be oldest-first: %+v", response.Items)
	}
	if response.Items[0].Summary != "会话创建" || response.Items[1].ActorType != "routing" {
		t.Errorf("projection fields missing: %+v", response.Items)
	}
	if repo.gotLimit != timelineDefaultLimit {
		t.Errorf("default limit should be %d, got %d", timelineDefaultLimit, repo.gotLimit)
	}
}

func TestTimelineAdapter_ParamValidation(t *testing.T) {
	cases := []struct {
		name string
		path string
		want int
	}{
		{"bad limit", "/x?limit=abc", http.StatusBadRequest},
		{"zero limit", "/x?limit=0", http.StatusBadRequest},
		{"negative offset", "/x?offset=-1", http.StatusBadRequest},
		{"ok capped limit", "/x?limit=99999", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &scriptedTimelineRepo{}
			adapter := NewTimelineAdapter(repo)
			ctx, recorder := newTimelineTestContext(t, tc.path)
			adapter.HandleListTimeline(ctx)
			if recorder.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, recorder.Code, recorder.Body.String())
			}
			if tc.want == http.StatusOK && repo.gotLimit != timelineMaxLimit {
				t.Errorf("limit should cap at %d, got %d", timelineMaxLimit, repo.gotLimit)
			}
		})
	}
}

func TestTimelineAdapter_NilRepoReturns503(t *testing.T) {
	adapter := NewTimelineAdapter(nil)
	ctx, recorder := newTimelineTestContext(t, "/x")
	adapter.HandleListTimeline(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
}

// failingTimelineRepo 覆盖 fakeEventRepo 的读失败，驱动 500 分支。
type failingTimelineRepo struct {
	fakeEventRepo
}

func (f *failingTimelineRepo) ListByConversation(context.Context, string, int, int) ([]domain.ConversationEvent, error) {
	return nil, errors.New("projection read failed")
}

func TestTimelineAdapter_RepoErrorReturns500(t *testing.T) {
	adapter := NewTimelineAdapter(&failingTimelineRepo{})
	ctx, recorder := newTimelineTestContext(t, "/x")
	adapter.HandleListTimeline(ctx)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", recorder.Code)
	}
}

func TestTimelineAdapter_EmptyConversationIDReturns400(t *testing.T) {
	adapter := NewTimelineAdapter(&scriptedTimelineRepo{})
	ctx, recorder := newTimelineTestContext(t, "/x")
	ctx.Params = gin.Params{{Key: "id", Value: ""}}
	adapter.HandleListTimeline(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestTimelineAdapter_PositiveOffsetPassedThrough(t *testing.T) {
	repo := &scriptedTimelineRepo{}
	adapter := NewTimelineAdapter(repo)
	ctx, recorder := newTimelineTestContext(t, "/x?offset=7")
	adapter.HandleListTimeline(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if repo.gotOffset != 7 {
		t.Fatalf("offset should pass through, got %d", repo.gotOffset)
	}
}
