package telemetry

import (
	"context"
	"time"

	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

const traceIDHeader = "X-Trace-ID"

// TraceIDFromContext 取 OTel span 的 trace_id（tracing 启用时由 otelgin
// 注入 request ctx）；无 span 返回空串。
func TraceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// FieldsFromContext 汇集结构化日志透传字段（O1 命名约定，names.go 常量）：
//   - request_id：RequestIDMiddleware 注入 gin ctx（生成或透传 X-Request-ID）
//   - tenant_id：auth 中间件盖进 request ctx 的租户（与仓储写入、事件盖章同源）
//   - trace_id：OTel span 优先，缺失时回退上游透传的 X-Trace-ID 头
//
// 空值不出现在结果里，避免日志行被空键淹没。
func FieldsFromContext(c *gin.Context) logrus.Fields {
	fields := logrus.Fields{}
	if v := c.GetString(FieldRequestID); v != "" {
		fields[FieldRequestID] = v
	}
	if c.Request != nil {
		if v := platformauth.TenantIDFromContext(c.Request.Context()); v != "" {
			fields[FieldTenantID] = v
		}
		if v := TraceIDFromContext(c.Request.Context()); v != "" {
			fields[FieldTraceID] = v
		}
	}
	if _, ok := fields[FieldTraceID]; !ok {
		if v := c.GetString(FieldTraceID); v != "" {
			fields[FieldTraceID] = v
		}
	}
	return fields
}

// AccessLogger 结构化访问日志：O1 透传字段 + 标准访问维度（method/path/
// status/latency/客户端 IP）。走 logrus 标准出口，与 InitLogger 按配置选定
// 的 JSON/text 格式、输出目标合一，替代 gin.Logger 的独立文本流。
func AccessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		fields := FieldsFromContext(c)
		fields["method"] = c.Request.Method
		fields["path"] = path
		if query != "" {
			fields["query"] = query
		}
		fields["status"] = c.Writer.Status()
		fields["latency_ms"] = float64(time.Since(start).Microseconds()) / 1000.0
		fields["client_ip"] = c.ClientIP()
		logrus.WithFields(fields).Info("http access")
	}
}
