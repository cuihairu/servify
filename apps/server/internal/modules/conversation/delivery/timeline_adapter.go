package delivery

import (
	"net/http"
	"strconv"
	"time"

	conversationapp "servify/apps/server/internal/modules/conversation/application"

	"github.com/gin-gonic/gin"
)

// TimelineAdapter 是 Service Timeline 的只读 HTTP 面（V1.0 收敛 B1-2，
// docs/v1-convergence-plan.md §3.1/W6）：GET /omni/sessions/:id/timeline，
// 按 conversation_id 读 conversation_events 投影流水。事件由
// EventBusSubscriber 投影写入，本适配器只读，不暴露 payload 原文
// （排障走库，展示走 event_type + summary）。
type TimelineAdapter struct {
	repo conversationapp.ConversationEventRepository
}

func NewTimelineAdapter(repo conversationapp.ConversationEventRepository) *TimelineAdapter {
	return &TimelineAdapter{repo: repo}
}

// TimelineEventDTO 单条服务过程事件；时间线展示字段，无内部载荷。
type TimelineEventDTO struct {
	EventType  string    `json:"event_type"`
	ActorType  string    `json:"actor_type"`
	ActorID    string    `json:"actor_id,omitempty"`
	Summary    string    `json:"summary"`
	OccurredAt time.Time `json:"occurred_at"`
}

// TimelineResponse 时间线响应：旧→新时序（展示即阅读顺序）。
type TimelineResponse struct {
	ConversationID string             `json:"conversation_id"`
	Items          []TimelineEventDTO `json:"items"`
	Count          int                `json:"count"`
}

const (
	timelineDefaultLimit = 50
	timelineMaxLimit     = 200
)

// HandleListTimeline 处理 GET /omni/sessions/:id/timeline。
func (a *TimelineAdapter) HandleListTimeline(c *gin.Context) {
	if a == nil || a.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Timeline service unavailable",
			"message": "timeline projection is not configured",
		})
		return
	}
	conversationID := c.Param("id")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation id is required"})
		return
	}
	limit := timelineDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		if parsed > timelineMaxLimit {
			parsed = timelineMaxLimit
		}
		limit = parsed
	}
	offset := 0
	if raw := c.Query("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
			return
		}
		offset = parsed
	}
	events, err := a.repo.ListByConversation(c.Request.Context(), conversationID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load timeline"})
		return
	}
	// 仓储按新→旧返回，时间线旧→新展示。
	items := make([]TimelineEventDTO, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		items = append(items, TimelineEventDTO{
			EventType:  events[i].EventType,
			ActorType:  events[i].ActorType,
			ActorID:    events[i].ActorID,
			Summary:    events[i].Summary,
			OccurredAt: events[i].OccurredAt,
		})
	}
	c.JSON(http.StatusOK, TimelineResponse{
		ConversationID: conversationID,
		Items:          items,
		Count:          len(items),
	})
}
