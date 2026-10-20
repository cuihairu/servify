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

// ChatConnectorHandler 暴露 quarantine 泳道与连接器运行态的运维面。
// 各面单独注入：未装配的依赖返回 404 而不是 panic。
type ChatConnectorHandler struct {
	lane    ChatConnectorQuarantineLane
	runtime ChatConnectorRuntimeControl
	replay  ChatConnectorReplayer
}

func NewChatConnectorHandler(lane ChatConnectorQuarantineLane, runtime ChatConnectorRuntimeControl, replay ChatConnectorReplayer) *ChatConnectorHandler {
	return &ChatConnectorHandler{lane: lane, runtime: runtime, replay: replay}
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
		group.GET("/quarantine", handler.ListQuarantine)
		group.GET("/quarantine/export", handler.ExportQuarantineCSV)
		group.POST("/quarantine/:id/dismiss", handler.DismissQuarantined)
		group.GET("/inbox/unforwarded", handler.ListUnforwarded)
		group.POST("/inbox/:id/replay", handler.ReplayInboxRow)
	}
}
