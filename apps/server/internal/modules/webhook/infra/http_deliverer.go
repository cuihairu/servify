package infra

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const defaultTimeout = 30 * time.Second

// HTTPDeliverer 执行签名后的 webhook POST。
type HTTPDeliverer struct {
	client *http.Client
	now    func() time.Time
}

func NewHTTPDeliverer() *HTTPDeliverer {
	return &HTTPDeliverer{
		client: &http.Client{
			Timeout:   defaultTimeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		now: time.Now,
	}
}

// NewHTTPDelivererWithClient 供测试注入受控 client。
func NewHTTPDelivererWithClient(client *http.Client) *HTTPDeliverer {
	return &HTTPDeliverer{client: client, now: time.Now}
}

func (d *HTTPDeliverer) Deliver(ctx context.Context, req application.DeliveryRequest) application.DeliveryResult {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := d.now()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return application.DeliveryResult{Error: fmt.Sprintf("build request: %v", err), DurationMs: elapsedMs(d.now, start)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Servify-Event", req.EventName)
	if req.EventID != "" {
		httpReq.Header.Set("X-Servify-Event-ID", req.EventID)
	}
	if req.DeliveryID > 0 {
		httpReq.Header.Set("X-Servify-Delivery-ID", strconv.FormatUint(req.DeliveryID, 10))
	}
	if req.Secret != "" {
		httpReq.Header.Set("X-Servify-Signature", webhookdelivery.SignatureHeader(req.Secret, req.Body, d.now()))
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return application.DeliveryResult{Error: err.Error(), DurationMs: elapsedMs(d.now, start)}
	}
	defer resp.Body.Close()
	// 丢弃响应体（限读防恶意大响应），保证连接可复用。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	result := application.DeliveryResult{
		Success:    success,
		StatusCode: resp.StatusCode,
		DurationMs: elapsedMs(d.now, start),
	}
	if !success {
		result.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return result
}

func elapsedMs(now func() time.Time, start time.Time) int64 {
	return now().Sub(start).Milliseconds()
}
