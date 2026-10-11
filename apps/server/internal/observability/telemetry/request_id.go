package telemetry

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

// RequestIDMiddleware generates a UUID request ID if not already present,
// sets it in the response header, and stores it in the Gin context for
// other middleware.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}

		// Set in response header for client-side correlation.
		c.Header(requestIDHeader, id)

		// Store in Gin context for other middleware.
		c.Set(FieldRequestID, id)

		// 上游透传的 X-Trace-ID 存 gin ctx 供 FieldsFromContext 兜底（无
		// OTel span 时日志侧 trace_id 来源）。
		if traceID := c.GetHeader(traceIDHeader); traceID != "" {
			c.Set(FieldTraceID, traceID)
		}

		c.Next()
	}
}
