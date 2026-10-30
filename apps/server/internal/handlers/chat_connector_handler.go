package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/channel"
	"servify/apps/server/internal/platform/channel/chatconnector"

	"github.com/gin-gonic/gin"
)

// 聊天连接器运维面（脱敏观测 + dead-letter / quarantine 处置）。
//
// 脱敏前提由存储面保证而不是这里过滤：inbox 行按契约只保存来源引用与
// 元数据（docs/chat-platform-integration-contract.md 的「原始负载本机加密
// ≤30 天、云备份/导出关闭」），从不保存负载文本或凭据。因此整行与整份
// CSV 都是脱敏产物；导出列清单显式列出可出内容面的字段。

const (
	// chatConnectorReloadTimeout 是运维触发的重启等待上限：一次长轮询加
	// 余量，避免一个卡住的轮询循环无限期挂住请求。
	chatConnectorReloadTimeout = 30 * time.Second
	// chatConnectorMaxRows 是单页上限，与 audit 列表口径一致。
	chatConnectorMaxRows = 500
)

// ChatConnectorQuarantineLane 是 quarantine 泳道的读取与处置面
// （由 *channel.InboxStore 实现）。
type ChatConnectorQuarantineLane interface {
	ListQuarantined(ctx context.Context, tenantID, workspaceID string, limit int) ([]channel.ChatConnectorInbox, error)
	Dismiss(ctx context.Context, tenantID, workspaceID string, id uint) error
}

// ChatConnectorRuntimeControl 是连接器监督器的运行态观测与重启面
// （由 *chatconnector.Supervisor 实现）。
type ChatConnectorRuntimeControl interface {
	Snapshot() []chatconnector.RunnerState
	Reload(ctx context.Context) error
}

// ChatConnectorReplayer 是 replay 泳道的读取与重放面（由
// app/server 的 *ChatInboxReplayer 实现：从留存原始负载重建事件并驱动进
// 正常流）。错误全部是哨兵——重放面不透出任何负载内容。
type ChatConnectorReplayer interface {
	ListUnforwarded(ctx context.Context, tenantID, workspaceID string, limit int) ([]channel.ChatConnectorInbox, error)
	ReplayInboxRow(ctx context.Context, tenantID, workspaceID string, id uint) error
}

// ChatConnectorInstallationStore 是安装映射的写入面（由
// *channel.InstallationStore 实现）。作用域由鉴权上下文注入，请求体改不动
// 租户/工作区。
type ChatConnectorInstallationStore interface {
	Save(ctx context.Context, inst channel.ChatConnectorInstallation) (channel.ChatConnectorInstallation, error)
}

// ChatConnectorCapabilityStore 是能力快照的写入面（由 *channel.CapabilityStore
// 实现）。快照按契约只能来自平台授权结果，所以请求侧必须给出观察时点。
type ChatConnectorCapabilityStore interface {
	Record(ctx context.Context, scope channel.CapabilityScope, caps channel.Capabilities) error
}

// ChatConnectorNotifier 是操作者公告的出站缝（由 app/server 的
// *chatConnectorNotifier 实现）。实现方按作用域解析安装行、把收件人钉在行
// 映射的会话上（契约：收件人绝不从请求隐式扩面）并驱动适配器的类型化
// Notify。返回的 SendResult 携带契约交付判定（accepted/unknown/rejected）。
type ChatConnectorNotifier interface {
	Notify(ctx context.Context, tenantID, workspaceID, platform, installationID string, req channel.NotifyRequest) (channel.SendResult, error)
}

// ChatConnectorHandler 暴露连接器运维面：脱敏观测、quarantine/replay 处置、
// 运行态重启，以及让安装映射与能力快照脱离裸 SQL 的配置写入面。
// 各面单独注入：未装配的依赖返回 404 而不是 panic。
type ChatConnectorHandler struct {
	lane          ChatConnectorQuarantineLane
	runtime       ChatConnectorRuntimeControl
	replay        ChatConnectorReplayer
	installations ChatConnectorInstallationStore
	grants        ChatConnectorCapabilityStore
	notifier      ChatConnectorNotifier
}

func NewChatConnectorHandler(
	lane ChatConnectorQuarantineLane,
	runtime ChatConnectorRuntimeControl,
	replay ChatConnectorReplayer,
	installations ChatConnectorInstallationStore,
	grants ChatConnectorCapabilityStore,
	notifier ChatConnectorNotifier,
) *ChatConnectorHandler {
	return &ChatConnectorHandler{
		lane: lane, runtime: runtime, replay: replay,
		installations: installations, grants: grants,
		notifier: notifier,
	}
}

// ListQuarantine 返回当前作用域的 quarantine 泳道（JSON 视图）。
func (h *ChatConnectorHandler) ListQuarantine(c *gin.Context) {
	if h == nil || h.lane == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector inbox unavailable", Message: "quarantine store not configured"})
		return
	}
	limit := chatConnectorListLimit(c)
	rows, err := h.lane.ListQuarantined(c.Request.Context(), platformauth.TenantIDFromContext(c.Request.Context()), platformauth.WorkspaceIDFromContext(c.Request.Context()), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list quarantined events", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, PaginatedResponse{Data: rows, Total: int64(len(rows)), Page: 1, PageSize: limit})
}

// ExportQuarantineCSV 导出当前作用域的 quarantine 泳道（CSV 视图）。
// 列清单固定为无内容面字段（见文件头说明）。
func (h *ChatConnectorHandler) ExportQuarantineCSV(c *gin.Context) {
	if h == nil || h.lane == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector inbox unavailable", Message: "quarantine store not configured"})
		return
	}
	rows, err := h.lane.ListQuarantined(c.Request.Context(), platformauth.TenantIDFromContext(c.Request.Context()), platformauth.WorkspaceIDFromContext(c.Request.Context()), chatConnectorMaxRows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list quarantined events", Message: err.Error()})
		return
	}

	header := []string{
		"id", "platform", "installation_id", "event_id", "event_type", "status",
		"conversation_platform_id", "sender_platform_id", "message_platform_id",
		"raw_payload_store", "raw_payload_object_id", "occurred_at", "received_at",
		"tenant_id", "workspace_id",
	}
	var buf bytes.Buffer
	writer := newCSVWriter(&buf)
	// 表头为固定 15 列（远小于 csv.Writer 内部缓冲），Write 不会失败；
	// 行写入与 Flush 的错误在下方统一处理。
	_ = writer.Write(header)
	for _, row := range rows {
		rec := []string{
			strconv.FormatUint(uint64(row.ID), 10),
			row.Platform, row.InstallationID, row.EventID, row.EventType, row.Status,
			row.ConversationPlatformID, row.SenderPlatformID, row.MessagePlatformID,
			row.RawPayloadStore, row.RawPayloadObjectID,
			row.OccurredAt.Format(time.RFC3339), row.ReceivedAt.Format(time.RFC3339),
			row.TenantID, row.WorkspaceID,
		}
		if err := writer.Write(rec); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
		return
	}

	filename := fmt.Sprintf("chat_connector_quarantine_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// DismissQuarantined 把一个 quarantine 行置为 dismissed：事件仍留库作证据，
// 但离开泳道，不再阻塞后续 triage（一次性处置，非回放）。
func (h *ChatConnectorHandler) DismissQuarantined(c *gin.Context) {
	if h == nil || h.lane == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector inbox unavailable", Message: "quarantine store not configured"})
		return
	}
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: "id must be a positive integer"})
		return
	}
	if err := h.lane.Dismiss(c.Request.Context(), platformauth.TenantIDFromContext(c.Request.Context()), platformauth.WorkspaceIDFromContext(c.Request.Context()), uint(id)); err != nil {
		switch {
		case isNotFoundError(err):
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "Inbox row not found", Message: "no quarantined row for this scope"})
		case errors.Is(err, channel.ErrInboxNotQuarantined):
			c.JSON(http.StatusConflict, ErrorResponse{Error: "Inbox row not quarantined", Message: err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to dismiss inbox row", Message: err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "Quarantined event dismissed"})
}

// ListUnforwarded 返回当前作用域的 replay 泳道：已提交但正常流送达未settled
// 的行（会话摄入失败或 commit 与送达之间的崩溃），最旧在前以便按到达序重放。
// 行本身无内容面（见文件头说明）。
func (h *ChatConnectorHandler) ListUnforwarded(c *gin.Context) {
	if h == nil || h.replay == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector replay unavailable", Message: "replay tooling not configured"})
		return
	}
	limit := chatConnectorListLimit(c)
	rows, err := h.replay.ListUnforwarded(c.Request.Context(), platformauth.TenantIDFromContext(c.Request.Context()), platformauth.WorkspaceIDFromContext(c.Request.Context()), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list unforwarded events", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, PaginatedResponse{Data: rows, Total: int64(len(rows)), Page: 1, PageSize: limit})
}

// ReplayInboxRow 把一个已提交行的留存原始负载重建为事件并驱动进正常流
// （重放进正常流，非直接写库）：正常流送达成功后该行离开 replay 泳道。
func (h *ChatConnectorHandler) ReplayInboxRow(c *gin.Context) {
	if h == nil || h.replay == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector replay unavailable", Message: "replay tooling not configured"})
		return
	}
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: "id must be a positive integer"})
		return
	}
	if err := h.replay.ReplayInboxRow(c.Request.Context(), platformauth.TenantIDFromContext(c.Request.Context()), platformauth.WorkspaceIDFromContext(c.Request.Context()), uint(id)); err != nil {
		code, resp := replayErrorResponse(err)
		c.JSON(code, resp)
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "Inbox event replayed"})
}

// replayErrorResponse 把重放哨兵映射成运营处置语义：泳道不可重放的是 409
// 状态冲突（留存对象过期/丢失、安装行已删、载荷已不可解析），其余才是故障。
// 哨兵分支先于通用 not-found 文本匹配（包装进来的「object not found」子串
// 是留存对象的问题，不是行的 404）。
func replayErrorResponse(err error) (int, ErrorResponse) {
	switch {
	case errors.Is(err, channel.ErrInboxNotCommitted):
		return http.StatusConflict, ErrorResponse{Error: "Inbox row not committed", Message: "only committed rows can be replayed"}
	case errors.Is(err, channel.ErrReplayPayloadUnavailable):
		return http.StatusConflict, ErrorResponse{Error: "Retained payload unavailable", Message: "the raw payload is gone, expired, or was never retained"}
	case errors.Is(err, channel.ErrReplaySourceGone):
		return http.StatusConflict, ErrorResponse{Error: "Connector installation gone", Message: "the installation row behind this event no longer exists"}
	case errors.Is(err, channel.ErrReplayMalformed):
		return http.StatusConflict, ErrorResponse{Error: "Retained payload no longer decodes", Message: "the raw payload no longer matches its platform format"}
	case errors.Is(err, channel.ErrReplayMismatch):
		return http.StatusConflict, ErrorResponse{Error: "Retained payload does not match row", Message: "the raw payload does not belong to this inbox row"}
	case errors.Is(err, channel.ErrReplayUnsupportedPlatform):
		return http.StatusConflict, ErrorResponse{Error: "No replay translator for platform", Message: "this runtime cannot rebuild this platform's events"}
	case isNotFoundError(err):
		return http.StatusNotFound, ErrorResponse{Error: "Inbox row not found", Message: "no inbox row for this scope"}
	default:
		return http.StatusInternalServerError, ErrorResponse{Error: "Failed to replay inbox row", Message: err.Error()}
	}
}

// ListRunners 返回每个已挂载连接器的运行态（平台/安装/作用域/状态/终止错误），
// 全部为无内容面字段；dead-letter 因此不必只存在于日志里。
func (h *ChatConnectorHandler) ListRunners(c *gin.Context) {
	if h == nil || h.runtime == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector runtime unavailable", Message: "connector supervisor not configured"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runners": h.runtime.Snapshot()})
}

// Reload 重启所有已挂载连接器：dead-letter 预算耗尽或行开关变更后的运维处置。
func (h *ChatConnectorHandler) ReloadConnectors(c *gin.Context) {
	if h == nil || h.runtime == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector runtime unavailable", Message: "connector supervisor not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), chatConnectorReloadTimeout)
	defer cancel()
	if err := h.runtime.Reload(ctx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusGatewayTimeout, ErrorResponse{Error: "Connector reload timed out", Message: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to reload connectors", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runners": h.runtime.Snapshot()})
}

// chatConnectorInstallationRequest 是安装映射写入请求体。tenant_id /
// workspace_id 刻意不在其中：作用域只来自鉴权上下文，请求体无法越权改写。
type chatConnectorInstallationRequest struct {
	Platform       string `json:"platform"`
	InstallationID string `json:"installation_id"`
	Conversation   struct {
		PlatformID string `json:"platform_id"`
		Kind       string `json:"kind"`
		IsGroup    bool   `json:"is_group"`
	} `json:"conversation"`
	UpdatesEndpoint string `json:"updates_endpoint"`
	TokenEnvVar     string `json:"token_env_var"`
	LongPollMS      int    `json:"long_poll_ms"`
	Enabled         bool   `json:"enabled"`
}

// SaveInstallation 写入或覆盖一条安装映射（upsert）。runner 只按 Enabled 行
// 挂载，所以「装上但没开」是默认安全态；写入后沿用 POST /reload 生效。
// 该组路由在 /api 的全局审计中间件之下，组授权变更因此有审计载体。
func (h *ChatConnectorHandler) SaveInstallation(c *gin.Context) {
	if h == nil || h.installations == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector configuration unavailable", Message: "installation store not configured"})
		return
	}
	var req chatConnectorInstallationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	inst := channel.ChatConnectorInstallation{
		TenantID:               platformauth.TenantIDFromContext(c.Request.Context()),
		WorkspaceID:            platformauth.WorkspaceIDFromContext(c.Request.Context()),
		Platform:               strings.TrimSpace(req.Platform),
		InstallationID:         strings.TrimSpace(req.InstallationID),
		ConversationPlatformID: strings.TrimSpace(req.Conversation.PlatformID),
		ConversationKind:       strings.TrimSpace(req.Conversation.Kind),
		ConversationIsGroup:    req.Conversation.IsGroup,
		UpdatesEndpoint:        strings.TrimSpace(req.UpdatesEndpoint),
		TokenEnvVar:            strings.TrimSpace(req.TokenEnvVar),
		LongPollMS:             req.LongPollMS,
		Enabled:                req.Enabled,
	}
	// 缺省回落存储列默认值，免得操作者重复填一个默认数。
	if inst.LongPollMS == 0 {
		inst.LongPollMS = channel.DefaultLongPollMS
	}
	// 校验先行：存储层同样校验，但这类错误在这里就该是 400 而不是 500。
	if err := inst.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid installation", Message: err.Error()})
		return
	}
	saved, err := h.installations.Save(c.Request.Context(), inst)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to save installation", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"installation": saved, "message": "Installation saved"})
}

// chatConnectorCapabilitiesRequest 是能力快照写入请求体。能力只能来自平台
// 授权结果，所以 verified_at 必填：没有观察时点的快照一律保持摄入关闭。
type chatConnectorCapabilitiesRequest struct {
	Platform               string   `json:"platform"`
	InstallationID         string   `json:"installation_id"`
	ConversationPlatformID string   `json:"conversation_platform_id"`
	ReceiveMode            string   `json:"receive_mode"`
	MessageScope           string   `json:"message_scope"`
	HistoryRead            bool     `json:"history_read"`
	EventTypes             []string `json:"event_types"`
	Threads                bool     `json:"threads"`
	Replies                bool     `json:"replies"`
	Attachments            bool     `json:"attachments"`
	Edits                  bool     `json:"edits"`
	Deletes                bool     `json:"deletes"`
	Reactions              bool     `json:"reactions"`
	SendText               bool     `json:"send_text"`
	SendRich               bool     `json:"send_rich"`
	RateLimit              string   `json:"rate_limit"`
	VerifiedAt             string   `json:"verified_at"`
}

// RecordCapabilities 写入或覆盖一条能力快照（upsert，新观察覆盖旧观察）。
func (h *ChatConnectorHandler) RecordCapabilities(c *gin.Context) {
	if h == nil || h.grants == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector configuration unavailable", Message: "capability store not configured"})
		return
	}
	var req chatConnectorCapabilitiesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	scope, caps, err := parseChatConnectorCapabilities(req,
		platformauth.TenantIDFromContext(c.Request.Context()),
		platformauth.WorkspaceIDFromContext(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid capabilities", Message: err.Error()})
		return
	}
	if err := h.grants.Record(c.Request.Context(), scope, caps); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to record capabilities", Message: err.Error()})
		return
	}
	// ingestion_enabled 是写后的操作者信号：这条快照是否足以打开摄入。
	c.JSON(http.StatusOK, gin.H{"ingestion_enabled": caps.IngestionEnabled(), "message": "Capability snapshot recorded"})
}

// parseChatConnectorCapabilities 把请求体转成契约快照并做词表校验。词表在这里
// 闭合校验而不是依赖存储层：CapabilityStore.Record 不做任何校验，拼错的
// receive_mode 会静默落库成未知字符串，把摄入关成 fail-closed 且无从看出原因。
func parseChatConnectorCapabilities(req chatConnectorCapabilitiesRequest, tenantID, workspaceID string) (channel.CapabilityScope, channel.Capabilities, error) {
	scope := channel.CapabilityScope{
		TenantID:               tenantID,
		WorkspaceID:            workspaceID,
		Platform:               strings.TrimSpace(req.Platform),
		InstallationID:         strings.TrimSpace(req.InstallationID),
		ConversationPlatformID: strings.TrimSpace(req.ConversationPlatformID),
	}
	switch {
	case scope.Platform == "":
		return channel.CapabilityScope{}, channel.Capabilities{}, errors.New("capabilities platform is required")
	case scope.InstallationID == "":
		return channel.CapabilityScope{}, channel.Capabilities{}, errors.New("capabilities installation id is required")
	case scope.ConversationPlatformID == "":
		return channel.CapabilityScope{}, channel.Capabilities{}, errors.New("capabilities conversation id is required")
	}

	receiveMode := channel.ReceiveMode(strings.TrimSpace(req.ReceiveMode))
	switch receiveMode {
	case channel.ReceiveModePolling, channel.ReceiveModeWebhook, channel.ReceiveModeStream, channel.ReceiveModeUnknown:
	default:
		return channel.CapabilityScope{}, channel.Capabilities{},
			fmt.Errorf("receive_mode %q must be one of polling, webhook, stream, unknown", req.ReceiveMode)
	}

	messageScope := channel.MessageScope(strings.TrimSpace(req.MessageScope))
	switch messageScope {
	case channel.MessageScopeMentionOnly, channel.MessageScopeAllAuthorizedGroups, channel.MessageScopeUnknown:
	default:
		return channel.CapabilityScope{}, channel.Capabilities{},
			fmt.Errorf("message_scope %q must be one of mention_only, all_authorized_groups, unknown", req.MessageScope)
	}

	eventTypes := make([]channel.EventType, 0, len(req.EventTypes))
	for _, raw := range req.EventTypes {
		switch channel.EventType(raw) {
		case channel.EventTypeMessageCreated, channel.EventTypeMessageEdited, channel.EventTypeMessageDeleted,
			channel.EventTypeReactionChanged, channel.EventTypeMemberChanged, channel.EventTypeUnknown:
		default:
			return channel.CapabilityScope{}, channel.Capabilities{},
				fmt.Errorf("event type %q is not in the contract vocabulary", raw)
		}
		eventTypes = append(eventTypes, channel.EventType(raw))
	}

	// 缺失与格式错误在这里同样处理：都是「没有可核对的观察时点」。
	verifiedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.VerifiedAt))
	if err != nil {
		return channel.CapabilityScope{}, channel.Capabilities{}, errors.New("verified_at is required in RFC3339 form")
	}

	return scope, channel.Capabilities{
		ReceiveMode:  receiveMode,
		MessageScope: messageScope,
		HistoryRead:  req.HistoryRead,
		EventTypes:   eventTypes,
		Threads:      req.Threads,
		Replies:      req.Replies,
		Attachments:  req.Attachments,
		Edits:        req.Edits,
		Deletes:      req.Deletes,
		Reactions:    req.Reactions,
		SendText:     req.SendText,
		SendRich:     req.SendRich,
		RateLimit:    strings.TrimSpace(req.RateLimit),
		VerifiedAt:   verifiedAt,
	}, nil
}

// chatConnectorNotifyRequest 是操作者公告请求体。target 刻意不在其中：收件人
// 恒由已存安装行钉住（契约：绝不从请求隐式扩面）；reason 是审计面要求的公告
// 动因；idempotency_key 是调用方稳定重试锚点，缺失即拒绝（自动生成会让操作者
// 的重试失去锚点）。两个适配器均为 TEXT 面，正文只接受 text。
type chatConnectorNotifyRequest struct {
	Platform       string `json:"platform"`
	InstallationID string `json:"installation_id"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
	Text           string `json:"text"`
}

// NotifyConnector 发送一条操作者/系统公告到安装行映射的会话（契约 79 行
// Notify 语义的操作载体）。出站能力门在适配器侧 fail-closed（无 SendText 授权
// 即不出网）；交付判定如实透出：accepted 携带平台消息 id，unknown（传输未证
// 实，请求可能已达平台）绝不盲目重发，rejected 是平台明确拒绝。该路由在 /api
// 的全局审计中间件之下，reason 随请求体入审计。
func (h *ChatConnectorHandler) NotifyConnector(c *gin.Context) {
	if h == nil || h.notifier == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Chat connector outbound unavailable", Message: "notify dispatcher not configured"})
		return
	}
	var req chatConnectorNotifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request", Message: err.Error()})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	installationID := strings.TrimSpace(req.InstallationID)
	key := strings.TrimSpace(req.IdempotencyKey)
	switch {
	case platform == "":
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid notify request", Message: "platform is required"})
		return
	case installationID == "":
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid notify request", Message: "installation_id is required"})
		return
	case strings.TrimSpace(req.Reason) == "":
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid notify request", Message: "reason is required"})
		return
	case key == "":
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid notify request", Message: "idempotency_key is required"})
		return
	case req.Text == "":
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid notify request", Message: "text is required"})
		return
	}
	result, err := h.notifier.Notify(c.Request.Context(),
		platformauth.TenantIDFromContext(c.Request.Context()),
		platformauth.WorkspaceIDFromContext(c.Request.Context()),
		platform, installationID,
		channel.NotifyRequest{
			Reason:         strings.TrimSpace(req.Reason),
			IdempotencyKey: key,
			Content:        channel.SendContent{Text: req.Text},
		})
	if err != nil {
		code, resp := notifyErrorResponse(err, result)
		c.JSON(code, resp)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":              result.Status,
		"platform_message_id": result.PlatformMessageID,
		"accepted_at":         result.AcceptedAt,
	})
}

// notifyErrorResponse 把出站失败映射成操作者处置语义：安装行缺失是 404，
// SendText 未授权（fail-closed 门，未出网）是 409，平台明确拒绝是 409 状态
// 冲突，传输不确定（unknown，请求可能已达平台）是 502——重发必须换锚点语义
// 而不是原样重试，其余按故障 500。
func notifyErrorResponse(err error, result channel.SendResult) (int, ErrorResponse) {
	switch {
	case errors.Is(err, channel.ErrNotifierInstallationNotFound):
		return http.StatusNotFound, ErrorResponse{Error: "Connector installation not found", Message: "no installation row for this scope"}
	case errors.Is(err, channel.ErrNotifierDisabled):
		return http.StatusConflict, ErrorResponse{Error: "Connector disabled", Message: "installation is turned off; no announcements until it is enabled"}
	case errors.Is(err, channel.ErrSendDisabled):
		return http.StatusConflict, ErrorResponse{Error: "Notify disabled", Message: "no SendText capability recorded for this installation"}
	case result.Status == channel.SendResultUnknown:
		return http.StatusBadGateway, ErrorResponse{Error: "Delivery uncertain", Message: "the platform may have received the announcement; do not retry blindly with the same outcome"}
	case result.Status == channel.SendResultRejected:
		return http.StatusConflict, ErrorResponse{Error: "Platform refused the announcement", Message: err.Error()}
	default:
		return http.StatusInternalServerError, ErrorResponse{Error: "Failed to notify", Message: err.Error()}
	}
}

// chatConnectorListLimit 取 limit 参数并夹在上限内。
func chatConnectorListLimit(c *gin.Context) int {
	limit := intQuery(c, "limit", 50)
	if limit > chatConnectorMaxRows {
		return chatConnectorMaxRows
	}
	return limit
}

// RegisterChatConnectorRoutes 注册聊天连接器运维路由。
// 权限由调用方的 RequireResourcePermission("chat_connectors") 门控：
// GET 映射 chat_connectors.read，POST 映射 chat_connectors.write。
func RegisterChatConnectorRoutes(r *gin.RouterGroup, handler *ChatConnectorHandler) {
	if r == nil || handler == nil {
		return
	}
	group := r.Group("/chat_connectors")
	{
		group.GET("/runners", handler.ListRunners)
		group.POST("/reload", handler.ReloadConnectors)
		group.POST("/installations", handler.SaveInstallation)
		group.POST("/capabilities", handler.RecordCapabilities)
		group.GET("/quarantine", handler.ListQuarantine)
		group.GET("/quarantine/export", handler.ExportQuarantineCSV)
		group.POST("/quarantine/:id/dismiss", handler.DismissQuarantined)
		group.GET("/inbox/unforwarded", handler.ListUnforwarded)
		group.POST("/inbox/:id/replay", handler.ReplayInboxRow)
		group.POST("/notify", handler.NotifyConnector)
	}
}
