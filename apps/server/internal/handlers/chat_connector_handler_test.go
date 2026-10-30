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
	"time"

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

// stubChatConnectorReplayer 记录调用参数并回放固定结果，驱动 replay 端点的
// 哨兵→状态码映射。
type stubChatConnectorReplayer struct {
	rows                                                     []channel.ChatConnectorInbox
	listErr                                                  error
	replayErr                                                error
	replayed                                                 []uint
	listTenant, listWorkspace, replayTenant, replayWorkspace string
	listLimit                                                int
}

func (s *stubChatConnectorReplayer) ListUnforwarded(_ context.Context, tenantID, workspaceID string, limit int) ([]channel.ChatConnectorInbox, error) {
	s.listTenant, s.listWorkspace, s.listLimit = tenantID, workspaceID, limit
	return s.rows, s.listErr
}

func (s *stubChatConnectorReplayer) ReplayInboxRow(_ context.Context, tenantID, workspaceID string, id uint) error {
	s.replayTenant, s.replayWorkspace = tenantID, workspaceID
	s.replayed = append(s.replayed, id)
	return s.replayErr
}

// stubChatConnectorInstallations 记录安装映射写入面调用并回放固定结果，驱动
// 写入端点的作用域投影、缺省值与错误映射。
type stubChatConnectorInstallations struct {
	saved   channel.ChatConnectorInstallation
	saveErr error
	calls   int
}

func (s *stubChatConnectorInstallations) Save(_ context.Context, inst channel.ChatConnectorInstallation) (channel.ChatConnectorInstallation, error) {
	s.saved = inst
	s.calls++
	if s.saveErr != nil {
		return channel.ChatConnectorInstallation{}, s.saveErr
	}
	inst.ID = 7
	return inst, nil
}

// stubChatConnectorCapabilities 记录能力快照写入面调用并回放固定结果。
type stubChatConnectorCapabilities struct {
	scope     channel.CapabilityScope
	caps      channel.Capabilities
	recordErr error
	records   int
}

func (s *stubChatConnectorCapabilities) Record(_ context.Context, scope channel.CapabilityScope, caps channel.Capabilities) error {
	s.scope = scope
	s.caps = caps
	s.records++
	return s.recordErr
}

// chatConnectorScopeMiddleware 把鉴权作用域注入请求上下文，与真实路由上的
// AuthMiddleware 同职责（测试面只钉 tenant-1/workspace-1）。
func chatConnectorScopeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(platformauth.ContextWithScope(c.Request.Context(), "tenant-1", "workspace-1"))
		c.Next()
	}
}

func chatConnectorRouter(lane ChatConnectorQuarantineLane, runtime ChatConnectorRuntimeControl, replay ...ChatConnectorReplayer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(chatConnectorScopeMiddleware())
	var replayer ChatConnectorReplayer
	if len(replay) > 0 {
		replayer = replay[0]
	}
	RegisterChatConnectorRoutes(&r.RouterGroup, NewChatConnectorHandler(lane, runtime, replayer, nil, nil, nil))
	return r
}

// chatConnectorConfigRouter 只注入配置写入面：避免观测/处置用例为两个写入端
// 点重复传四个实参。
func chatConnectorConfigRouter(installs ChatConnectorInstallationStore, grants ChatConnectorCapabilityStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(chatConnectorScopeMiddleware())
	RegisterChatConnectorRoutes(&r.RouterGroup, NewChatConnectorHandler(nil, nil, nil, installs, grants, nil))
	return r
}

// chatConnectorNotifyRouter 只注入出站公告面。
func chatConnectorNotifyRouter(notifier ChatConnectorNotifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(chatConnectorScopeMiddleware())
	RegisterChatConnectorRoutes(&r.RouterGroup, NewChatConnectorHandler(nil, nil, nil, nil, nil, notifier))
	return r
}

func doChatConnectorRequest(r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	r.ServeHTTP(w, req)
	return w
}

func doChatConnectorJSONRequest(r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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
	RegisterChatConnectorRoutes(nil, NewChatConnectorHandler(&stubChatConnectorLane{}, nil, nil, nil, nil, nil))
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
		"GET /chat_connectors/inbox/unforwarded",
		"POST /chat_connectors/inbox/1/replay",
		"POST /chat_connectors/installations",
		"POST /chat_connectors/capabilities",
	} {
		parts := strings.SplitN(target, " ", 2)
		if w := doChatConnectorRequest(r, parts[0], parts[1]); w.Code != http.StatusNotFound {
			t.Fatalf("%s code=%d, want 404 (unconfigured surface)", target, w.Code)
		}
	}
}

func TestChatConnectorHandlerListUnforwarded(t *testing.T) {
	rep := &stubChatConnectorReplayer{rows: []channel.ChatConnectorInbox{
		{ID: 1, TenantID: "tenant-1", WorkspaceID: "workspace-1", Platform: "telegram",
			EventID: "evt-1", Status: channel.InboxStatusCommitted, ForwardedAt: nil},
	}}
	r := chatConnectorRouter(nil, nil, rep)

	w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/inbox/unforwarded")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if rep.listTenant != "tenant-1" || rep.listWorkspace != "workspace-1" || rep.listLimit != 50 {
		t.Fatalf("scope/limit projected = %q/%q/%d", rep.listTenant, rep.listWorkspace, rep.listLimit)
	}
	for _, want := range []string{`"total":1`, "evt-1", "telegram"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("body missing %q: %s", want, w.Body.String())
		}
	}

	// limit 覆盖与夹取沿 quarantine 同一口径
	w = doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/inbox/unforwarded?limit=3")
	if w.Code != http.StatusOK || rep.listLimit != 3 {
		t.Fatalf("limit=3 code=%d seen=%d", w.Code, rep.listLimit)
	}
	w = doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/inbox/unforwarded?limit=99999")
	if w.Code != http.StatusOK || rep.listLimit != chatConnectorMaxRows {
		t.Fatalf("limit clamp code=%d seen=%d, want %d", w.Code, rep.listLimit, chatConnectorMaxRows)
	}

	// 存储错误 → 500
	rep.listErr = errors.New("db down")
	if w := doChatConnectorRequest(r, http.MethodGet, "/chat_connectors/inbox/unforwarded"); w.Code != http.StatusInternalServerError {
		t.Fatalf("list error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerReplayInboxRow(t *testing.T) {
	rep := &stubChatConnectorReplayer{}
	r := chatConnectorRouter(nil, nil, rep)

	w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/inbox/7/replay")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if len(rep.replayed) != 1 || rep.replayed[0] != 7 {
		t.Fatalf("replayed ids = %v, want [7]", rep.replayed)
	}
	if rep.replayTenant != "tenant-1" || rep.replayWorkspace != "workspace-1" {
		t.Fatalf("scope projected = %q/%q", rep.replayTenant, rep.replayWorkspace)
	}

	// 坏 id → 400
	for _, bad := range []string{"notanumber", "0"} {
		if w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/inbox/"+bad+"/replay"); w.Code != http.StatusBadRequest {
			t.Fatalf("bad id %q code=%d, want 400", bad, w.Code)
		}
	}

	// 哨兵 → 运营处置语义（行不存在 404 / 泳道不可重放 409 / 其余 500）
	cases := []struct {
		err     error
		code    int
		wantErr string
	}{
		{errors.New("record not found"), http.StatusNotFound, "Inbox row not found"},
		{channel.ErrInboxNotCommitted, http.StatusConflict, "Inbox row not committed"},
		{channel.ErrReplayPayloadUnavailable, http.StatusConflict, "Retained payload unavailable"},
		{channel.ErrReplaySourceGone, http.StatusConflict, "Connector installation gone"},
		{channel.ErrReplayMalformed, http.StatusConflict, "Retained payload no longer decodes"},
		{channel.ErrReplayMismatch, http.StatusConflict, "Retained payload does not match row"},
		{channel.ErrReplayUnsupportedPlatform, http.StatusConflict, "No replay translator for platform"},
		{errors.New("boom"), http.StatusInternalServerError, "Failed to replay inbox row"},
	}
	for _, tc := range cases {
		rep.replayErr = tc.err
		w := doChatConnectorRequest(r, http.MethodPost, "/chat_connectors/inbox/7/replay")
		if w.Code != tc.code {
			t.Fatalf("%v code=%d, want %d", tc.err, w.Code, tc.code)
		}
		if !strings.Contains(w.Body.String(), tc.wantErr) {
			t.Fatalf("%v body missing %q: %s", tc.err, tc.wantErr, w.Body.String())
		}
	}

	// 未装配 replay 面 → 404
	if w := doChatConnectorRequest(chatConnectorRouter(nil, nil), http.MethodPost, "/chat_connectors/inbox/7/replay"); w.Code != http.StatusNotFound {
		t.Fatalf("unconfigured replay code=%d, want 404", w.Code)
	}
}

// validInstallationBody 是安装映射写入请求的合法基线，校验用例按字段替换它。
const validInstallationBody = `{"platform":"telegram","installation_id":"12345:AAAg",` +
	`"conversation":{"platform_id":"-100123","kind":"group","is_group":true},` +
	`"updates_endpoint":"https://api.telegram.org","token_env_var":"TELEGRAM_BOT_TOKEN",` +
	`"long_poll_ms":25000}`

func TestChatConnectorHandlerSaveInstallation(t *testing.T) {
	installs := &stubChatConnectorInstallations{}
	r := chatConnectorConfigRouter(installs, nil)

	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations", validInstallationBody)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	saved := installs.saved
	if saved.TenantID != "tenant-1" || saved.WorkspaceID != "workspace-1" {
		t.Fatalf("scope projected = %q/%q, want tenant-1/workspace-1", saved.TenantID, saved.WorkspaceID)
	}
	if saved.Platform != "telegram" || saved.InstallationID != "12345:AAAg" {
		t.Fatalf("identity drifted: %+v", saved)
	}
	if saved.ConversationPlatformID != "-100123" || saved.ConversationKind != "group" || !saved.ConversationIsGroup {
		t.Fatalf("conversation drifted: %+v", saved)
	}
	if saved.UpdatesEndpoint != "https://api.telegram.org" || saved.TokenEnvVar != "TELEGRAM_BOT_TOKEN" {
		t.Fatalf("wiring drifted: %+v", saved)
	}
	if saved.Enabled {
		t.Fatal("a newly written installation must stay inert (Enabled defaults false)")
	}
	if installs.calls != 1 {
		t.Fatalf("save calls = %d, want 1", installs.calls)
	}
	// upsert 后操作者能看到落库行（含自增 id）
	if !strings.Contains(w.Body.String(), `"id":7`) {
		t.Fatalf("response must return the saved row: %s", w.Body.String())
	}

	// 作用域只来自鉴权上下文：body 里的 tenant/workspace 不能越权改写
	body := validInstallationBody[:len(validInstallationBody)-1] + `,"tenant_id":"evil","workspace_id":"evil"}`
	w = doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations", body)
	if w.Code != http.StatusOK || installs.saved.TenantID != "tenant-1" || installs.saved.WorkspaceID != "workspace-1" {
		t.Fatalf("scope leaked from body: code=%d %+v", w.Code, installs.saved)
	}

	// 显式开启（Phase 0 联调就是「装上就开」）
	w = doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations",
		strings.Replace(validInstallationBody, `"long_poll_ms":25000`, `"long_poll_ms":25000,"enabled":true`, 1))
	if w.Code != http.StatusOK || !installs.saved.Enabled {
		t.Fatalf("enabled write code=%d %+v", w.Code, installs.saved)
	}

	// long_poll_ms 缺省回落存储列默认值
	w = doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations",
		strings.Replace(validInstallationBody, `,"long_poll_ms":25000`, "", 1))
	if w.Code != http.StatusOK || installs.saved.LongPollMS != channel.DefaultLongPollMS {
		t.Fatalf("default long poll code=%d longPoll=%d, want %d",
			w.Code, installs.saved.LongPollMS, channel.DefaultLongPollMS)
	}
}

func TestChatConnectorHandlerSaveInstallationValidation(t *testing.T) {
	installs := &stubChatConnectorInstallations{}
	r := chatConnectorConfigRouter(installs, nil)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"malformed json", `{not json`, "Invalid request"},
		{"missing platform", `{"installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":25000}`, "installation platform is required"},
		{"missing installation id", `{"platform":"telegram","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":25000}`, "installation id is required"},
		{"missing conversation id", `{"platform":"telegram","installation_id":"i","conversation":{"kind":"group"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":25000}`, "installation conversation id is required"},
		{"missing conversation kind", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":25000}`, "installation conversation kind is required"},
		{"missing updates endpoint", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"token_env_var":"T","long_poll_ms":25000}`, "installation updates endpoint is required"},
		{"missing token env var", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","long_poll_ms":25000}`, "installation token env var is required"},
		{"blank token env var", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","token_env_var":"   ","long_poll_ms":25000}`, "installation token env var is required"},
		{"negative long poll", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":-1}`, "installation long poll -1ms must be within 1..30000ms"},
		{"long poll above cap", `{"platform":"telegram","installation_id":"i","conversation":{"platform_id":"g","kind":"group"},"updates_endpoint":"u","token_env_var":"T","long_poll_ms":60000}`, "installation long poll 60000ms must be within 1..30000ms"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installs.calls = 0
			w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%s, want 400", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("body missing %q: %s", tc.want, w.Body.String())
			}
			if installs.calls != 0 {
				t.Fatalf("invalid request must not reach the store (calls=%d)", installs.calls)
			}
		})
	}
}

func TestChatConnectorHandlerSaveInstallationStoreError(t *testing.T) {
	installs := &stubChatConnectorInstallations{saveErr: errors.New("db down")}
	r := chatConnectorConfigRouter(installs, nil)

	if w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/installations", validInstallationBody); w.Code != http.StatusInternalServerError {
		t.Fatalf("store error code=%d, want 500", w.Code)
	}
}

func TestChatConnectorHandlerRecordCapabilities(t *testing.T) {
	grants := &stubChatConnectorCapabilities{}
	r := chatConnectorConfigRouter(nil, grants)

	body := `{"platform":"telegram","installation_id":"12345:AAAg","conversation_platform_id":"-100123",` +
		`"receive_mode":"polling","message_scope":"all_authorized_groups","history_read":true,` +
		`"event_types":["message.created","message.edited","message.deleted","reaction.changed","member.changed"],` +
		`"threads":false,"replies":true,"attachments":true,"edits":true,"deletes":false,"reactions":true,` +
		`"send_text":true,"send_rich":true,"rate_limit":"30 messages/min",` +
		`"verified_at":"2026-10-20T10:00:00Z"}`
	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/capabilities", body)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	scope := grants.scope
	if scope.TenantID != "tenant-1" || scope.WorkspaceID != "workspace-1" ||
		scope.Platform != "telegram" || scope.InstallationID != "12345:AAAg" ||
		scope.ConversationPlatformID != "-100123" {
		t.Fatalf("scope projected = %+v, want tenant-1/workspace-1/telegram/12345:AAAg/-100123", scope)
	}
	caps := grants.caps
	if caps.ReceiveMode != channel.ReceiveModePolling || caps.MessageScope != channel.MessageScopeAllAuthorizedGroups {
		t.Fatalf("grant drifted: %+v", caps)
	}
	if !caps.IngestionEnabled() {
		t.Fatalf("a recorded snapshot with a real observation time must enable ingestion: %+v", caps)
	}
	if len(caps.EventTypes) != 5 || caps.EventTypes[0] != channel.EventTypeMessageCreated ||
		caps.EventTypes[4] != channel.EventTypeMemberChanged {
		t.Fatalf("event types drifted: %+v", caps.EventTypes)
	}
	if !caps.HistoryRead || !caps.Replies || !caps.Attachments || !caps.Edits || !caps.Reactions ||
		!caps.SendText || !caps.SendRich {
		t.Fatalf("granted flags lost: %+v", caps)
	}
	if caps.Threads || caps.Deletes {
		t.Fatalf("unset flags leaked: %+v", caps)
	}
	if caps.RateLimit != "30 messages/min" {
		t.Fatalf("rate limit = %q", caps.RateLimit)
	}
	if grants.records != 1 || grants.caps.VerifiedAt.IsZero() {
		t.Fatalf("records=%d verifiedAt=%v, want 1 record with a non-zero observation time",
			grants.records, grants.caps.VerifiedAt)
	}
	// 写后信号：快照是否足以打开摄入
	if !strings.Contains(w.Body.String(), `"ingestion_enabled":true`) {
		t.Fatalf("response must report ingestion readiness: %s", w.Body.String())
	}

	// unknown 词表值允许记录（平台授权结果本身含糊），但必须保持摄入关闭
	w = doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/capabilities",
		`{"platform":"telegram","installation_id":"i","conversation_platform_id":"g",`+
			`"receive_mode":"unknown","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("unknown mode code=%d body=%s", w.Code, w.Body.String())
	}
	if grants.caps.IngestionEnabled() {
		t.Fatalf("an unknown receive mode must keep ingestion closed: %+v", grants.caps)
	}
	if !strings.Contains(w.Body.String(), `"ingestion_enabled":false`) {
		t.Fatalf("response must report the closed gate: %s", w.Body.String())
	}
}

func TestChatConnectorHandlerRecordCapabilitiesValidation(t *testing.T) {
	grants := &stubChatConnectorCapabilities{}
	r := chatConnectorConfigRouter(nil, grants)
	valid := `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g",` +
		`"receive_mode":"polling","message_scope":"all_authorized_groups","event_types":["message.created"],` +
		`"verified_at":"2026-10-20T10:00:00Z"}`

	cases := []struct {
		name string
		body string
		want string
	}{
		{"malformed json", `{not json`, "Invalid request"},
		{"missing platform", `{"installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, "capabilities platform is required"},
		{"missing installation id", `{"platform":"telegram","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, "capabilities installation id is required"},
		{"missing conversation id", `{"platform":"telegram","installation_id":"i","receive_mode":"polling","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, "capabilities conversation id is required"},
		{"blank platform", `{"platform":"  ","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, "capabilities platform is required"},
		{"unknown receive mode token", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"webhooks","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, `receive_mode \"webhooks\" must be one of`},
		{"missing receive mode", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`, `receive_mode \"\" must be one of`},
		{"unknown message scope token", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"all_groups","verified_at":"2026-10-20T10:00:00Z"}`, `message_scope \"all_groups\" must be one of`},
		{"missing message scope", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","verified_at":"2026-10-20T10:00:00Z"}`, `message_scope \"\" must be one of`},
		{"event type outside vocabulary", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only","event_types":["message.sent"],"verified_at":"2026-10-20T10:00:00Z"}`, `event type \"message.sent\" is not in the contract vocabulary`},
		{"missing verified at", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only"}`, "verified_at is required in RFC3339 form"},
		{"malformed verified at", `{"platform":"telegram","installation_id":"i","conversation_platform_id":"g","receive_mode":"polling","message_scope":"mention_only","verified_at":"2026/10/20"}`, "verified_at is required in RFC3339 form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grants.records = 0
			w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/capabilities", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%s, want 400", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("body missing %q: %s", tc.want, w.Body.String())
			}
			if grants.records != 0 {
				t.Fatalf("invalid request must not reach the store (records=%d)", grants.records)
			}
		})
	}

	// 合法基线必须过：校验用例不得误伤正常写入
	if w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/capabilities", valid); w.Code != http.StatusOK {
		t.Fatalf("valid body code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestChatConnectorHandlerRecordCapabilitiesStoreError(t *testing.T) {
	grants := &stubChatConnectorCapabilities{recordErr: errors.New("db down")}
	r := chatConnectorConfigRouter(nil, grants)

	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/capabilities",
		`{"platform":"telegram","installation_id":"i","conversation_platform_id":"g",`+
			`"receive_mode":"polling","message_scope":"mention_only","verified_at":"2026-10-20T10:00:00Z"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("store error code=%d, want 500", w.Code)
	}
}

// stubChatConnectorNotifier 记录操作者公告调用并回放固定结果，驱动 /notify 的
// 错误与交付判定映射（作用域投影 + 请求体透传）。
type stubChatConnectorNotifier struct {
	result                                    channel.SendResult
	err                                       error
	calls                                     int
	tenant, workspace, platform, installation string
	req                                       channel.NotifyRequest
}

func (s *stubChatConnectorNotifier) Notify(_ context.Context, tenantID, workspaceID, platform, installationID string, req channel.NotifyRequest) (channel.SendResult, error) {
	s.calls++
	s.tenant, s.workspace, s.platform, s.installation = tenantID, workspaceID, platform, installationID
	s.req = req
	return s.result, s.err
}

func TestChatConnectorHandlerNotify(t *testing.T) {
	validBody := `{"platform":"telegram","installation_id":"inst-1","reason":"maintenance","idempotency_key":"op-1","text":"system going down"}`
	cases := []struct {
		name       string
		notifier   *stubChatConnectorNotifier
		wantStatus int
		wantErr    string
	}{
		{"accepted", &stubChatConnectorNotifier{result: channel.SendResult{Status: channel.SendResultAccepted, PlatformMessageID: "123", AcceptedAt: time.Now()}}, http.StatusOK, ""},
		{"unknown", &stubChatConnectorNotifier{result: channel.SendResult{Status: channel.SendResultUnknown}, err: errors.New("transport failed")}, http.StatusBadGateway, "Delivery uncertain"},
		{"rejected", &stubChatConnectorNotifier{result: channel.SendResult{Status: channel.SendResultRejected}, err: errors.New("chat not found")}, http.StatusConflict, "Platform refused"},
		{"send disabled", &stubChatConnectorNotifier{err: channel.ErrSendDisabled}, http.StatusConflict, "Notify disabled"},
		{"installation not found", &stubChatConnectorNotifier{err: channel.ErrNotifierInstallationNotFound}, http.StatusNotFound, "Connector installation not found"},
		{"installation disabled", &stubChatConnectorNotifier{err: channel.ErrNotifierDisabled}, http.StatusConflict, "Connector disabled"},
		{"store failure", &stubChatConnectorNotifier{err: errors.New("db down")}, http.StatusInternalServerError, "Failed to notify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chatConnectorNotifyRouter(tc.notifier)
			w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/notify", validBody)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantErr != "" && !strings.Contains(w.Body.String(), tc.wantErr) {
				t.Fatalf("body %q missing %q", w.Body.String(), tc.wantErr)
			}
		})
	}
}

func TestChatConnectorHandlerNotifyAcceptedBody(t *testing.T) {
	acceptedAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	stub := &stubChatConnectorNotifier{result: channel.SendResult{Status: channel.SendResultAccepted, PlatformMessageID: "123", AcceptedAt: acceptedAt}}
	r := chatConnectorNotifyRouter(stub)
	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/notify",
		`{"platform":"telegram","installation_id":"inst-1","reason":"maintenance","idempotency_key":"op-1","text":"hello"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	for _, want := range []string{`"status":"accepted"`, `"platform_message_id":"123"`, `"accepted_at":"2026-10-30T12:00:00Z"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("accepted body missing %q: %s", want, w.Body.String())
		}
	}
}

func TestChatConnectorHandlerNotifyValidation(t *testing.T) {
	cases := []struct{ name, body, wantErr string }{
		{"bad json", `{`, "Invalid request"},
		{"missing platform", `{"installation_id":"i","reason":"r","idempotency_key":"k","text":"t"}`, "platform is required"},
		{"missing installation", `{"platform":"telegram","reason":"r","idempotency_key":"k","text":"t"}`, "installation_id is required"},
		{"missing reason", `{"platform":"telegram","installation_id":"i","idempotency_key":"k","text":"t"}`, "reason is required"},
		{"missing key", `{"platform":"telegram","installation_id":"i","reason":"r","text":"t"}`, "idempotency_key is required"},
		{"missing text", `{"platform":"telegram","installation_id":"i","reason":"r","idempotency_key":"k"}`, "text is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chatConnectorNotifyRouter(&stubChatConnectorNotifier{})
			w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/notify", tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantErr) {
				t.Fatalf("body %q missing %q", w.Body.String(), tc.wantErr)
			}
		})
	}
}

func TestChatConnectorHandlerNotifyUnconfigured(t *testing.T) {
	r := chatConnectorNotifyRouter(nil)
	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/notify",
		`{"platform":"telegram","installation_id":"i","reason":"r","idempotency_key":"k","text":"t"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unconfigured status = %d, want 404", w.Code)
	}
}

func TestChatConnectorHandlerNotifyScopeAndBody(t *testing.T) {
	stub := &stubChatConnectorNotifier{result: channel.SendResult{Status: channel.SendResultAccepted, PlatformMessageID: "1", AcceptedAt: time.Now()}}
	r := chatConnectorNotifyRouter(stub)
	body := `{"platform":"telegram","installation_id":"inst-9","reason":"ops drill","idempotency_key":"op-9","text":"hello"}`
	w := doChatConnectorJSONRequest(r, http.MethodPost, "/chat_connectors/notify", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	if stub.tenant != "tenant-1" || stub.workspace != "workspace-1" {
		t.Fatalf("scope from context = %s/%s, want tenant-1/workspace-1", stub.tenant, stub.workspace)
	}
	if stub.platform != "telegram" || stub.installation != "inst-9" {
		t.Fatalf("identity = %s/%s, want telegram/inst-9", stub.platform, stub.installation)
	}
	if stub.req.Reason != "ops drill" || stub.req.IdempotencyKey != "op-9" || stub.req.Content.Text != "hello" {
		t.Fatalf("notify request drifted: %+v", stub.req)
	}
	if stub.req.Target != "" {
		t.Fatalf("target must be pinned by the dispatcher, not the handler: %q", stub.req.Target)
	}
}
