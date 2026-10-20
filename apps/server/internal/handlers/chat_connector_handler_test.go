package handlers

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/chatconnector"

	"github.com/gin-gonic/gin"
)

// stubChatConnectorLane 记录调用参数并回放固定结果，驱动 handler 的错误映射。
type stubChatConnectorLane struct {
	rows             []channel.ChatConnectorInbox
	listErr          error
	dismissErr       error
	dismissed        []uint
	listTenant       string
	listWorkspace    string
	listLimit        int
	dismissTenant    string
	dismissWorkspace string
}

func (s *stubChatConnectorLane) ListQuarantined(_ context.Context, tenantID, workspaceID string, limit int) ([]channel.ChatConnectorInbox, error) {
	s.listTenant, s.listWorkspace, s.listLimit = tenantID, workspaceID, limit
	return s.rows, s.listErr
}

func (s *stubChatConnectorLane) Dismiss(_ context.Context, tenantID, workspaceID string, id uint) error {
	s.dismissTenant, s.dismissWorkspace = tenantID, workspaceID
	s.dismissed = append(s.dismissed, id)
	return s.dismissErr
}

// stubChatConnectorRuntime 回放 supervisor 的 Snapshot/Reload 面。
type stubChatConnectorRuntime struct {
	snapshot  []chatconnector.RunnerState
	reloadErr error
	reloads   int
}

func (s *stubChatConnectorRuntime) Snapshot() []chatconnector.RunnerState { return s.snapshot }

func (s *stubChatConnectorRuntime) Reload(context.Context) error {
	s.reloads++
	return s.reloadErr
}

func chatConnectorRouter(lane ChatConnectorQuarantineLane, runtime ChatConnectorRuntimeControl) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-1", "workspace-1"))
		c.Next()
	})
	RegisterChatConnectorRoutes(&r.RouterGroup, NewChatConnectorHandler(lane, runtime))
	return r
}

func doChatConnectorRequest(r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestChatConnectorHandlerListQuarantine(t *testing.T) {
	lane := &stubChatConnectorLane{rows: []channel.ChatConnectorInbox{
		{ID: 2, TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", EventID: "evt-2", Status: channel.InboxStatusQuarantined},
		{ID: 1, TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", EventID: "evt-1", Status: channel.InboxStatusQuarantined},
	}}
	r := chatConnectorRouter(lane, nil)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if lane.listTenant != "tenant-1" || lane.listWorkspace != "workspace-1" {
		t.Fatalf("scope projected = %q/%q, want tenant-1/workspace-1", lane.listTenant, lane.listWorkspace)
	}
	if lane.listLimit != 50 {
		t.Fatalf("default limit = %d, want 50", lane.listLimit)
	}
	body := w.Body.String()
	for _, want := range []string{`"total":2`, "evt-2", "quarantined"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}

	// limit 覆盖：合法值透传，非法值回落默认
	w = doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine?limit=7")
	if w.Code != http.StatusOK || lane.listLimit != 7 {
		t.Fatalf("limit=7 code=%d seen=%d", w.Code, lane.listLimit)
	}
	w = doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine?limit=notanumber")
	if w.Code != http.StatusOK || lane.listLimit != 50 {
		t.Fatalf("invalid limit code=%d seen=%d, want fallback 50", w.Code, lane.listLimit)
	}

	// store 错误 → 500
	lane.listErr = errors.New("db down")
	if w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine"); w.Code != http.StatusInternalServerError {
		t.Fatalf("list error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerExportQuarantineCSV(t *testing.T) {
	lane := &stubChatConnectorLane{rows: []channel.ChatConnectorInbox{
		{ID: 3, TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "memex", InstallationID: "inst-1",
			EventID: "evt-3", EventType: "unknown", Status: channel.InboxStatusQuarantined,
			ConversationPlatformID: "grp-1", SenderPlatformID: "user-1", MessagePlatformID: "msg-1"},
	}}
	r := chatConnectorRouter(lane, nil)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine/export")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type = %q, want text/csv", ct)
	}
	body := w.Body.String()
	// 脱敏导出：行本身按契约就是无内容面（无负载文本、无凭据），导出列
	// 只含引用与元数据
	for _, want := range []string{"id,platform,installation_id", "evt-3", "memex", "quarantined"} {
		if !strings.Contains(body, want) {
			t.Fatalf("csv missing %q: %s", want, body)
		}
	}
	if strings.Count(body, "\n") != 2 { // header + one row
		t.Fatalf("csv line count wrong: %q", body)
	}

	// store 错误 → 500
	lane.listErr = errors.New("db down")
	if w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine/export"); w.Code != http.StatusInternalServerError {
		t.Fatalf("export error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerExportQuarantineCSVWriteErrors(t *testing.T) {
	hscovSetSeam(t, &newCSVWriter, func(io.Writer) *csv.Writer {
		return csv.NewWriter(hscovFailingWriter{})
	})
	lane := &stubChatConnectorLane{rows: []channel.ChatConnectorInbox{{ID: 1}}}
	r := chatConnectorRouter(lane, nil)

	if w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine/export"); w.Code != http.StatusInternalServerError {
		t.Fatalf("write error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerExportQuarantineCSVRowWriteError(t *testing.T) {
	// 行写失败（区别于 Flush 失败）：字段长度越过 csv.Writer 内部缓冲，
	// Write 立即返回底层错误
	hscovSetSeam(t, &newCSVWriter, func(io.Writer) *csv.Writer {
		return csv.NewWriter(hscovFailingWriter{})
	})
	lane := &stubChatConnectorLane{rows: []channel.ChatConnectorInbox{
		{ID: 1, EventID: strings.Repeat("x", 6000)},
	}}
	r := chatConnectorRouter(lane, nil)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine/export")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to write csv") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestChatConnectorHandlerListQuarantineClampsLimit(t *testing.T) {
	lane := &stubChatConnectorLane{}
	r := chatConnectorRouter(lane, nil)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/quarantine?limit=999999")
	if w.Code != http.StatusOK || lane.listLimit != 500 {
		t.Fatalf("code=%d limit=%d, want 500 (upper clamp)", w.Code, lane.listLimit)
	}
}

func TestRegisterChatConnectorRoutesNilArguments(t *testing.T) {
	// 两个 nil 守卫：注册入口必须容忍半装配参数，而不是 panic
	RegisterChatConnectorRoutes(nil, NewChatConnectorHandler(&stubChatConnectorLane{}, nil))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterChatConnectorRoutes(&r.RouterGroup, nil)
	if len(r.Routes()) != 0 {
		t.Fatalf("nil handler must register nothing, routes = %v", r.Routes())
	}
}

func TestChatConnectorHandlerDismiss(t *testing.T) {
	lane := &stubChatConnectorLane{}
	r := chatConnectorRouter(lane, nil)

	w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/quarantine/42/dismiss")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(lane.dismissed) != 1 || lane.dismissed[0] != 42 {
		t.Fatalf("dismissed = %v, want [42]", lane.dismissed)
	}
	if lane.dismissTenant != "tenant-1" || lane.dismissWorkspace != "workspace-1" {
		t.Fatalf("scope projected = %q/%q", lane.dismissTenant, lane.dismissWorkspace)
	}

	cases := []struct {
		name       string
		target     string
		dismissErr error
		want       int
	}{
		{"unknown or cross-scope id", "/chat_connectors/quarantine/99/dismiss", errors.New("record not found"), http.StatusNotFound},
		{"not in quarantine lane", "/chat_connectors/quarantine/42/dismiss", channel.ErrInboxNotQuarantined, http.StatusConflict},
		{"store failure", "/chat_connectors/quarantine/42/dismiss", errors.New("db down"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lane.dismissErr = tc.dismissErr
			defer func() { lane.dismissErr = nil }()
			if w := doChatConnectorRequest(r, http.MethodPost, tc.target); w.Code != tc.want {
				t.Fatalf("code=%d body=%s, want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}

	for _, bad := range []string{"/chat_connectors/quarantine/notanumber/dismiss", "/chat_connectors/quarantine/0/dismiss"} {
		if w := doChatConnectorRequest(r, http.MethodPost, bad); w.Code != http.StatusBadRequest {
			t.Fatalf("%s code=%d, want 400", bad, w.Code)
		}
	}
}

func TestChatConnectorHandlerRunners(t *testing.T) {
	rt := &stubChatConnectorRuntime{snapshot: []chatconnector.RunnerState{
		{Platform: "memex", InstallationID: "inst-1", TenantID: "tenant-1", WorkspaceID: "workspace-1", Status: chatconnector.RunnerStatusExited, Error: "dead-lettered"},
	}}
	r := chatConnectorRouter(nil, rt)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/runners")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"runners"`, "inst-1", "exited", "dead-lettered"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestChatConnectorHandlerReload(t *testing.T) {
	rt := &stubChatConnectorRuntime{snapshot: []chatconnector.RunnerState{
		{Platform: "memex", InstallationID: "inst-1", Status: chatconnector.RunnerStatusRunning},
	}}
	r := chatConnectorRouter(nil, rt)

	w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/reload")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if rt.reloads != 1 {
		t.Fatalf("reloads = %d, want 1", rt.reloads)
	}
	if !strings.Contains(w.Body.String(), `"runners"`) {
		t.Fatalf("reload response must include the post-reload snapshot: %s", w.Body.String())
	}

	// 停机等待超时 → 504；其余错误 → 500
	rt.reloadErr = context.DeadlineExceeded
	if w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/reload"); w.Code != http.StatusGatewayTimeout {
		t.Fatalf("deadline code=%d, want 504", w.Code)
	}
	rt.reloadErr = errors.New("db down")
	if w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/reload"); w.Code != http.StatusInternalServerError {
		t.Fatalf("store error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerUnconfiguredSurfacesReturn404(t *testing.T) {
	r := chatConnectorRouter(nil, nil)
	for _, target := range []string{
		"GET /chat_connectors/quarantine",
		"GET /chat_connectors/quarantine/export",
		"POST /chat_connectors/quarantine/1/dismiss",
		"GET /chat_connectors/runners",
		"POST /chat_connectors/reload",
	} {
		parts := strings.SplitN(target, " ", 2)
		if w := doChatConnectorRequest(r, parts[0], parts[1]); w.Code != http.StatusNotFound {
			t.Fatalf("%s code=%d, want 404 (unconfigured surface)", target, w.Code)
		}
	}
}
