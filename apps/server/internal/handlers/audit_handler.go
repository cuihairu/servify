package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	auditplatform "servify/apps/server/internal/platform/audit"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
)

type AuditHandler struct {
	service auditplatform.QueryService
}

func NewAuditHandler(service auditplatform.QueryService) *AuditHandler {
	return &AuditHandler{service: service}
}

func (h *AuditHandler) List(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}

	query, respErr := buildAuditListQuery(c)
	if respErr != nil {
		c.JSON(respErr.Code, respErr)
		return
	}

	items, total, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to list audit logs", Message: err.Error()})
		return
	}

	c.JSON(http.StatusOK, PaginatedResponse{
		Data:     items,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	})
}

func (h *AuditHandler) ExportCSV(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}

	query, respErr := buildAuditListQuery(c)
	if respErr != nil {
		c.JSON(respErr.Code, respErr)
		return
	}

	// intQuery 恒返回正数（解析失败/非正数时回落 fallback=1000），无需再 clamp 下限。
	limit := intQuery(c, "limit", 1000)
	if limit > 5000 {
		limit = 5000
	}
	query.Page = 1
	query.PageSize = limit

	items, _, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to export audit logs", Message: err.Error()})
		return
	}

	var buf bytes.Buffer
	writer := newCSVWriter(&buf)
	header := []string{
		"id", "created_at", "action", "resource_type", "resource_id", "principal_kind", "actor_user_id",
		"success", "status_code", "route", "method", "request_id", "tenant_id", "workspace_id",
		"client_ip", "user_agent", "request_json", "before_json", "after_json",
	}
	// 表头为固定 19 列（约 160 字节，远小于 csv.Writer 内部 4KB bufio 缓冲），
	// Write 不会失败；行写入与 Flush 的错误在下方统一处理。
	_ = writer.Write(header)
	for _, item := range items {
		actorUserID := ""
		if item.ActorUserID != nil {
			actorUserID = fmt.Sprintf("%d", *item.ActorUserID)
		}
		row := []string{
			fmt.Sprintf("%d", item.ID),
			item.CreatedAt.Format(time.RFC3339),
			item.Action,
			item.ResourceType,
			item.ResourceID,
			item.PrincipalKind,
			actorUserID,
			strconv.FormatBool(item.Success),
			strconv.Itoa(item.StatusCode),
			item.Route,
			item.Method,
			item.RequestID,
			item.TenantID,
			item.WorkspaceID,
			item.ClientIP,
			item.UserAgent,
			item.RequestJSON,
			item.BeforeJSON,
			item.AfterJSON,
		}
		if err := writer.Write(row); err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to write csv", Message: err.Error()})
		return
	}

	filename := fmt.Sprintf("audit_logs_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

func (h *AuditHandler) Get(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}

	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: "id must be a positive integer"})
		return
	}

	item, err := h.service.Get(c.Request.Context(), uint(id), auditplatform.QueryScope{
		TenantID:    platformauth.TenantIDFromContext(c.Request.Context()),
		WorkspaceID: platformauth.WorkspaceIDFromContext(c.Request.Context()),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get audit log", Message: err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit log not found", Message: "no matching audit log found"})
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *AuditHandler) GetDiff(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit service unavailable", Message: "audit query service not configured"})
		return
	}

	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid id", Message: "id must be a positive integer"})
		return
	}

	item, err := h.service.Get(c.Request.Context(), uint(id), auditplatform.QueryScope{
		TenantID:    platformauth.TenantIDFromContext(c.Request.Context()),
		WorkspaceID: platformauth.WorkspaceIDFromContext(c.Request.Context()),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get audit log", Message: err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Audit log not found", Message: "no matching audit log found"})
		return
	}

	diff, err := buildAuditDiff(item.BeforeJSON, item.AfterJSON)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Audit diff unavailable", Message: err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"audit": item,
		"diff":  diff,
	})
}

func RegisterAuditRoutes(r *gin.RouterGroup, handler *AuditHandler) {
	if r == nil || handler == nil {
		return
	}
	audit := r.Group("/audit")
	{
		audit.GET("/logs", handler.List)
		audit.GET("/logs/export", handler.ExportCSV)
		audit.GET("/logs/:id", handler.Get)
		audit.GET("/logs/:id/diff", handler.GetDiff)
	}
}

func intQuery(c *gin.Context, key string, fallback int) int {
	if c == nil {
		return fallback
	}
	v := c.Query(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func buildAuditListQuery(c *gin.Context) (auditplatform.ListQuery, *ErrorResponse) {
	query := auditplatform.ListQuery{
		Action:        c.Query("action"),
		ResourceType:  c.Query("resource_type"),
		ResourceID:    c.Query("resource_id"),
		PrincipalKind: c.Query("principal_kind"),
		TenantID:      platformauth.TenantIDFromContext(c.Request.Context()),
		WorkspaceID:   platformauth.WorkspaceIDFromContext(c.Request.Context()),
		Page:          intQuery(c, "page", 1),
		PageSize:      intQuery(c, "page_size", 20),
	}

	if v := c.Query("actor_user_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return auditplatform.ListQuery{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid actor_user_id", Message: err.Error()}
		}
		uid := uint(id)
		query.ActorUserID = &uid
	}

	if v := c.Query("success"); v != "" {
		switch v {
		case "true":
			b := true
			query.Success = &b
		case "false":
			b := false
			query.Success = &b
		default:
			return auditplatform.ListQuery{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid success", Message: "use true or false"}
		}
	}

	if v := c.Query("from"); v != "" {
		tm, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return auditplatform.ListQuery{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid from", Message: err.Error()}
		}
		query.From = &tm
	}
	if v := c.Query("to"); v != "" {
		tm, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return auditplatform.ListQuery{}, &ErrorResponse{Code: http.StatusBadRequest, Error: "Invalid to", Message: err.Error()}
		}
		query.To = &tm
	}
	return query, nil
}

func buildAuditDiff(beforeJSON, afterJSON string) (gin.H, error) {
	before, err := parseAuditJSON(beforeJSON)
	if err != nil {
		return nil, err
	}
	after, err := parseAuditJSON(afterJSON)
	if err != nil {
		return nil, err
	}
	changes := diffAuditValues(before, after, "")
	return gin.H{
		"has_before":    before != nil,
		"has_after":     after != nil,
		"changed":       len(changes) > 0,
		"change_count":  len(changes),
		"changed_paths": collectAuditDiffPaths(changes),
		"changes":       changes,
	}, nil
}

func parseAuditJSON(raw string) (interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func diffAuditValues(before, after interface{}, prefix string) []gin.H {
	beforeMap, beforeOK := before.(map[string]interface{})
	afterMap, afterOK := after.(map[string]interface{})
	if beforeOK && afterOK {
		keys := map[string]struct{}{}
		for k := range beforeMap {
			keys[k] = struct{}{}
		}
		for k := range afterMap {
			keys[k] = struct{}{}
		}
		changes := make([]gin.H, 0, len(keys))
		for key := range keys {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			changes = append(changes, diffAuditValues(beforeMap[key], afterMap[key], path)...)
		}
		return changes
	}

	if !auditValuesEqual(before, after) {
		return []gin.H{{
			"path":   prefix,
			"type":   auditDiffType(before, after),
			"before": before,
			"after":  after,
		}}
	}
	return nil
}

func auditValuesEqual(before, after interface{}) bool {
	return strings.TrimSpace(toAuditJSON(before)) == strings.TrimSpace(toAuditJSON(after))
}

func toAuditJSON(v interface{}) string {
	if v == nil {
		return "null"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func auditDiffType(before, after interface{}) string {
	switch {
	case before == nil && after != nil:
		return "added"
	case before != nil && after == nil:
		return "removed"
	default:
		return "updated"
	}
}

func collectAuditDiffPaths(changes []gin.H) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if path, ok := change["path"].(string); ok && path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
