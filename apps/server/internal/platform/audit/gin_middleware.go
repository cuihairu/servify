package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

const (
	beforeKey       = "audit.before"
	afterKey        = "audit.after"
	actionKey       = "audit.action"
	resourceTypeKey = "audit.resource_type"
	resourceIDKey   = "audit.resource_id"
	requestMetaKey  = "audit.request_metadata"
)

// Options 控制 audit 中间件的记录面。
type Options struct {
	// AuditFailures 为 true 时 4xx/5xx 响应同样落审计（安全面需要失败
	// 登录等事件留痕）；默认 false 保持管理面"只记成功写操作"的既有行为。
	AuditFailures bool
}

func Middleware(recorder Recorder) gin.HandlerFunc {
	return MiddlewareWithOptions(recorder, Options{})
}

func MiddlewareWithOptions(recorder Recorder, opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		if recorder == nil || !shouldAuditMethod(c.Request.Method) {
			c.Next()
			return
		}

		requestJSON := captureRequestBody(c)
		c.Next()

		if c.Writer.Status() >= http.StatusBadRequest && !opts.AuditFailures {
			return
		}

		entry := Entry{
			ActorUserID:   actorUserID(c),
			PrincipalKind: stringValue(c, "principal_kind"),
			Action:        contextOrDefault(c, actionKey, inferAction(c.FullPath(), c.Request.Method)),
			ResourceType:  contextOrDefault(c, resourceTypeKey, inferResourceType(c.FullPath())),
			ResourceID:    contextOrDefault(c, resourceIDKey, inferResourceID(c)),
			Route:         c.FullPath(),
			Method:        strings.ToUpper(strings.TrimSpace(c.Request.Method)),
			StatusCode:    c.Writer.Status(),
			Success:       c.Writer.Status() < http.StatusBadRequest,
			RequestID:     c.GetHeader("X-Request-ID"),
			ClientIP:      c.ClientIP(),
			UserAgent:     c.Request.UserAgent(),
			TenantID:      stringValue(c, "tenant_id"),
			WorkspaceID:   stringValue(c, "workspace_id"),
			RequestJSON:   mergeRequestJSON(requestJSON, requestMetadata(c)),
			BeforeJSON:    contextJSON(c, beforeKey),
			AfterJSON:     contextJSON(c, afterKey),
		}

		if err := recorder.Record(c.Request.Context(), entry); err != nil {
			logrus.WithError(err).Warn("failed to persist audit log")
		}
	}
}

func SetBefore(c *gin.Context, value interface{}) {
	if c != nil {
		c.Set(beforeKey, value)
	}
}

func SetAfter(c *gin.Context, value interface{}) {
	if c != nil {
		c.Set(afterKey, value)
	}
}

func SetAction(c *gin.Context, value string) {
	if c != nil {
		c.Set(actionKey, strings.TrimSpace(value))
	}
}

func SetResourceType(c *gin.Context, value string) {
	if c != nil {
		c.Set(resourceTypeKey, strings.TrimSpace(value))
	}
}

func SetResourceID(c *gin.Context, value string) {
	if c != nil {
		c.Set(resourceIDKey, strings.TrimSpace(value))
	}
}

func MergeRequestMetadata(c *gin.Context, values map[string]interface{}) {
	if c == nil || len(values) == 0 {
		return
	}
	merged := map[string]interface{}{}
	if existing, ok := c.Get(requestMetaKey); ok {
		if typed, ok := existing.(map[string]interface{}); ok {
			for key, value := range typed {
				merged[key] = value
			}
		}
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			if strings.TrimSpace(text) == "" {
				continue
			}
			merged[key] = strings.TrimSpace(text)
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return
	}
	c.Set(requestMetaKey, merged)
}

func shouldAuditMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func captureRequestBody(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return redactJSONText(strings.TrimSpace(string(body)))
}

func actorUserID(c *gin.Context) *uint {
	if c == nil {
		return nil
	}
	v, ok := c.Get("user_id")
	if !ok {
		return nil
	}
	id, ok := v.(uint)
	if !ok {
		return nil
	}
	return &id
}

func stringValue(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	v, ok := c.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func contextOrDefault(c *gin.Context, key, fallback string) string {
	if value := stringValue(c, key); value != "" {
		return value
	}
	return fallback
}

func contextJSON(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	v, ok := c.Get(key)
	if !ok || v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return redactJSONText(string(data))
}

func requestMetadata(c *gin.Context) map[string]interface{} {
	if c == nil {
		return nil
	}
	value, ok := c.Get(requestMetaKey)
	if !ok || value == nil {
		return nil
	}
	typed, ok := value.(map[string]interface{})
	if !ok || len(typed) == 0 {
		return nil
	}
	metadata := make(map[string]interface{}, len(typed))
	for key, item := range typed {
		metadata[key] = item
	}
	return metadata
}

func mergeRequestJSON(raw string, metadata map[string]interface{}) string {
	raw = strings.TrimSpace(raw)
	if len(metadata) == 0 {
		return raw
	}
	if raw == "" {
		data, err := json.Marshal(metadata)
		if err != nil {
			return ""
		}
		return redactJSONText(string(data))
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		data, marshalErr := json.Marshal(map[string]interface{}{
			"request":  raw,
			"metadata": metadata,
		})
		if marshalErr != nil {
			return raw
		}
		return redactJSONText(string(data))
	}

	object, ok := payload.(map[string]interface{})
	if !ok {
		data, err := json.Marshal(map[string]interface{}{
			"request":  payload,
			"metadata": metadata,
		})
		if err != nil {
			return raw
		}
		return redactJSONText(string(data))
	}

	var conflictMetadata map[string]interface{}
	for key, value := range metadata {
		existing, exists := object[key]
		if !exists {
			object[key] = value
			continue
		}
		if jsonValueEquals(existing, value) {
			continue
		}
		if conflictMetadata == nil {
			conflictMetadata = map[string]interface{}{}
		}
		conflictMetadata[key] = value
	}
	if len(conflictMetadata) > 0 {
		object["_audit"] = mergeObjectField(object["_audit"], conflictMetadata)
	}

	data, err := json.Marshal(object)
	if err != nil {
		return raw
	}
	return redactJSONText(string(data))
}

func mergeObjectField(existing interface{}, values map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	if typed, ok := existing.(map[string]interface{}); ok {
		for key, value := range typed {
			result[key] = value
		}
	}
	for key, value := range values {
		result[key] = value
	}
	return result
}

func jsonValueEquals(left, right interface{}) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

// jsonMarshal 是包级 seam（默认 json.Marshal）：redactJSONText 的输入已
// 先通过 Unmarshal 校验、redactValue 只变换结构不改类型，重序列化生产路径
// 恒成功，测试注入失败以覆盖回落分支。
var jsonMarshal = json.Marshal

func redactJSONText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}

	redacted, changed := redactValue(payload)
	if !changed {
		return raw
	}

	data, err := jsonMarshal(redacted)
	if err != nil {
		return raw
	}
	return string(data)
}

func redactValue(value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		changed := false
		for key, item := range typed {
			if shouldRedactKey(key) {
				typed[key] = "[REDACTED]"
				changed = true
				continue
			}
			next, nestedChanged := redactValue(item)
			if nestedChanged {
				typed[key] = next
				changed = true
			}
		}
		return typed, changed
	case []interface{}:
		changed := false
		for i, item := range typed {
			next, nestedChanged := redactValue(item)
			if nestedChanged {
				typed[i] = next
				changed = true
			}
		}
		return typed, changed
	default:
		return value, false
	}
}

func shouldRedactKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	switch normalized {
	case "password", "passwd", "secret", "clientsecret", "apikey", "token", "accesstoken", "refreshtoken", "authorization":
		return true
	default:
		return strings.HasSuffix(normalized, "secret") || strings.HasSuffix(normalized, "token") || strings.HasSuffix(normalized, "apikey")
	}
}

func inferResourceType(route string) string {
	parts := routeParts(route)
	if len(parts) == 0 {
		return "unknown"
	}
	if parts[0] == "api" {
		parts = parts[1:]
	}
	if len(parts) > 0 && parts[0] == "v1" {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "unknown"
	}
	switch parts[0] {
	case "omni":
		if len(parts) > 1 {
			return sanitizeSegment(parts[1])
		}
	case "apps":
		if len(parts) > 1 {
			return "app_" + sanitizeSegment(parts[1])
		}
	}
	return sanitizeSegment(parts[0])
}

func inferAction(route, method string) string {
	parts := routeParts(route)
	if len(parts) == 0 {
		return strings.ToLower(method)
	}
	resource := inferResourceType(route)
	last := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], ":") {
			continue
		}
		last = sanitizeSegment(parts[i])
		break
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost:
		if last == "" || last == resource {
			return resource + ".create"
		}
		return resource + "." + last
	case http.MethodPut, http.MethodPatch:
		if last == "" || last == resource {
			return resource + ".update"
		}
		return resource + "." + last
	case http.MethodDelete:
		return resource + ".delete"
	default:
		return resource + "." + strings.ToLower(method)
	}
}

func inferResourceID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, key := range []string{"id", "ticket_id", "session_id", "recordingID", "recording_id", "protocol"} {
		if v := strings.TrimSpace(c.Param(key)); v != "" {
			return v
		}
	}
	return ""
}

func routeParts(route string) []string {
	trimmed := strings.Trim(strings.TrimSpace(route), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func sanitizeSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	segment = strings.TrimPrefix(segment, ":")
	segment = strings.ReplaceAll(segment, "-", "_")
	return segment
}
