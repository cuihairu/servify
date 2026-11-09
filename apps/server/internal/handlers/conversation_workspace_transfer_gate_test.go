package handlers

// 转接 gate 的 HTTP 面（并发转接与目标校验）：无效/跨作用域/不可接目标映射
// 明确状态码，真实 API 并发转接恰一方获胜且时间线与最终分配一致。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationdomain "servify/apps/server/internal/modules/conversation/domain"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"

	"github.com/gin-gonic/gin"
)

type stubTransferValidator struct{ err error }

func (v *stubTransferValidator) ValidateTransferTarget(ctx context.Context, agentID uint, tenantID, workspaceID string) error {
	return v.err
}

func transferRequest(t *testing.T, router *gin.Engine, sessionID string, toAgentID uint) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]uint{"to_agent_id": toAgentID})
	req := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/"+sessionID+"/transfer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestConversationWorkspaceHandler_TransferFailureResults(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"target not found", conversationapp.ErrTransferTargetNotFound, http.StatusNotFound},
		{"target out of scope", conversationapp.ErrTransferTargetOutOfScope, http.StatusForbidden},
		{"target unavailable", conversationapp.ErrTransferTargetUnavailable, http.StatusConflict},
		{"concurrent transfer", conversationapp.ErrConcurrentTransfer, http.StatusConflict},
		{"unmapped failure", errors.New("validator exploded"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			db := newConversationWorkspaceTestDB(t)
			service := seedConversation(t, db)
			service.AttachTransferTargetValidator(&stubTransferValidator{err: tc.err})
			router := gin.New()
			RegisterConversationWorkspaceRoutes(router.Group("/api"),
				NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil, nil))

			w := transferRequest(t, router, "sess-1", 2)
			if w.Code != tc.status {
				t.Fatalf("expected %d, got %d: %s", tc.status, w.Code, w.Body.String())
			}
			// 校验失败不落任何写：会话状态原样。
			dto, err := service.GetConversation(t.Context(), "sess-1")
			if err != nil {
				t.Fatalf("reload conversation: %v", err)
			}
			if dto.Status != "active" {
				t.Fatalf("failed transfer must not mutate status, got %q", dto.Status)
			}
		})
	}
}

// barrierConversationRepo 在 CAS 前拦停两个请求，确保两者读到同一前置状态
// 后再放行（否则串行时序下双方都合法成功）。barrier 只覆盖一次性转接竞争，
// 其余调用直通真实仓储。
type barrierConversationRepo struct {
	conversationapp.ConversationRepository
	mu      sync.Mutex
	arrived int
	release chan struct{}
	once    sync.Once
}

func (r *barrierConversationRepo) TransferAgentAtomic(ctx context.Context, conversationID string, expectedAgentID *uint, newAgentID uint, newStatus conversationdomain.ConversationStatus) (bool, error) {
	r.mu.Lock()
	r.arrived++
	if r.arrived == 2 {
		r.once.Do(func() { close(r.release) })
	}
	r.mu.Unlock()
	// 伴跑请求缺席时 10s 后放行，避免测试挂死。
	select {
	case <-r.release:
	case <-time.After(10 * time.Second):
	}
	return r.ConversationRepository.TransferAgentAtomic(ctx, conversationID, expectedAgentID, newAgentID, newStatus)
}

// TestConversationWorkspaceHandler_ConcurrentTransferSingleWinner 真实 API 面
// 并发转接：同一会话并发转给两个目标，前置状态一致，CAS 保证恰一方获胜
// （200），败者 409 且不留系统消息；最终分配与时间线一致。
func TestConversationWorkspaceHandler_ConcurrentTransferSingleWinner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newConversationWorkspaceTestDB(t)
	service := seedConversationOn(t, db, &barrierConversationRepo{
		ConversationRepository: conversationinfra.NewGormRepository(db),
		release:                make(chan struct{}),
	})
	// 转接前置：先派给坐席 1（有 agent 参与人，CAS 有非空预期）。
	router := gin.New()
	RegisterConversationWorkspaceRoutes(router.Group("/api"),
		NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(service), nil, nil))
	assignBody, _ := json.Marshal(map[string]uint{"agent_id": 1})
	assignReq := httptest.NewRequest(http.MethodPost, "/api/omni/sessions/sess-1/assign", bytes.NewReader(assignBody))
	assignReq.Header.Set("Content-Type", "application/json")
	assignW := httptest.NewRecorder()
	router.ServeHTTP(assignW, assignReq)
	if assignW.Code != http.StatusOK {
		t.Fatalf("assign precondition failed: %d %s", assignW.Code, assignW.Body.String())
	}

	ready := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, target := range []uint{2, 3} {
		wg.Add(1)
		go func(slot int, to uint) {
			defer wg.Done()
			<-ready
			codes[slot] = transferRequest(t, router, "sess-1", to).Code
		}(i, target)
	}
	close(ready)
	wg.Wait()

	wins, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			wins++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d (codes %v)", code, codes)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("expected exactly one winner and one conflict, got %v", codes)
	}

	// 最终分配与时间线一致：恰好一条系统消息，且指向最终坐席。
	dto, err := service.GetConversation(t.Context(), "sess-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if dto.Status != "transferred" {
		t.Fatalf("expected transferred, got %q", dto.Status)
	}
	var finalAgent uint
	for _, p := range dto.Participants {
		if p.Role == "agent" && p.UserID != nil {
			finalAgent = *p.UserID
		}
	}
	if finalAgent != 2 && finalAgent != 3 {
		t.Fatalf("expected final agent 2 or 3, got %d", finalAgent)
	}
	msgs, err := service.ListRecentMessages(t.Context(), "sess-1", 50)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	systemMsgs := 0
	for _, m := range msgs {
		if m.Kind == "system" {
			systemMsgs++
		}
	}
	if systemMsgs != 1 {
		t.Fatalf("expected exactly one transfer system message, got %d", systemMsgs)
	}
}
