package errors

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// httpStatusError 让按响应状态码记录的错误复用 AppError 的分类与标签。
type httpStatusError struct {
	status int
}

func (e *httpStatusError) Error() string {
	return "http response " + strconv.Itoa(e.status)
}

// classifyHTTPStatus 把响应状态码映射为错误分类：502/504 语义上是上游依赖
// 失败（反向代理/上游超时），归类 dependency；其余 5xx 归类 system。
func classifyHTTPStatus(status int) (Severity, Category) {
	if status == http.StatusBadGateway || status == http.StatusGatewayTimeout {
		return SeverityDependency, CategoryNetwork
	}
	return SeveritySystem, CategoryInternal
}

// RecordHTTPStatus 在错误统一出口（HTTP 响应）按最终状态码记录一次服务端
// 错误。status < 500 不计数：4xx 是请求方错误，且限流已有
// ratelimit_dropped_total 单独计数，避免同一事件双计。
func RecordHTTPStatus(status int) {
	if status < http.StatusInternalServerError {
		return
	}
	sev, cat := classifyHTTPStatus(status)
	RecordError(New(&httpStatusError{status: status}, sev, cat,
		WithModule("http"),
		WithCode(strconv.Itoa(status)),
	))
}

// StatusMiddleware 在响应完成后按最终状态码记录 5xx 错误。与 HTTPMetrics
// 中间件并排挂载，一处接线覆盖全部路由，是 errors_total 的统一出口。
func StatusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		RecordHTTPStatus(c.Writer.Status())
	}
}
