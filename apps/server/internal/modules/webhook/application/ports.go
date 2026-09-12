package application

import (
	"context"
	"time"
)

// DeliveryRequest 描述一次出站 webhook POST。
type DeliveryRequest struct {
	URL        string
	Secret     string
	EventName  string
	EventID    string
	DeliveryID uint64 // 订阅路径 = delivery 行 id；automation/测试等一次性投递 = 0
	Body       []byte
	Timeout    time.Duration
}

// DeliveryResult 一次投递尝试的结果。
type DeliveryResult struct {
	Success    bool
	StatusCode int
	DurationMs int64
	Error      string
}

// Deliverer 执行一次签名后的 webhook POST。订阅系统与 automation 动作共用。
type Deliverer interface {
	Deliver(ctx context.Context, req DeliveryRequest) DeliveryResult
}

// Processor 供后台 worker 驱动的投递处理入口。
type Processor interface {
	ProcessDueDeliveries(ctx context.Context, now time.Time) int
}
