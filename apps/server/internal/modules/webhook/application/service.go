package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
)

// ErrNilRequest 空请求体。
var ErrNilRequest = errors.New("nil request")

// ErrNotFound 目标记录不存在。
var ErrNotFound = errors.New("webhook record not found")

// retryBackoff 第 n 次失败后的退避序列（下标 attempt-1）。
var retryBackoff = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	10 * time.Hour,
	24 * time.Hour,
}

// maxAttempts 投递总次数上限；达到即置 dead。
var maxAttempts = len(retryBackoff)

// noRetryStatuses 收到这些状态码说明端点配置类错误，重试无意义。
var noRetryStatuses = map[int]bool{
	401: true,
	403: true,
	404: true,
	405: true,
	410: true,
}

// payloadTruncateLimit 投递记录里 payload 的最大保留字节数。
const payloadTruncateLimit = 64 << 10

// 测试注入点（默认值保持生产行为）：
//   - readRandom：GenerateSecret 的熵源，注入失败覆盖错误分支；
//   - newSecret：端点密钥生成，注入失败覆盖创建/轮换错误分支；
//   - marshalEnvelope：事件信封序列化，注入失败覆盖序列化错误分支。
var (
	readRandom      = rand.Read
	newSecret       = GenerateSecret
	marshalEnvelope = json.Marshal
)

// Service webhook 应用服务：事件入队、端点管理、投递处理。
type Service struct {
	repo      Repository
	logger    *logrus.Logger
	deliverer Deliverer
	now       func() time.Time

	mu sync.Mutex
}

func NewService(repo Repository, logger *logrus.Logger) *Service {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Service{repo: repo, logger: logger, now: time.Now}
}

// SetDeliverer 注入 HTTP 投递器（测试可注入 fake）。
func (s *Service) SetDeliverer(d Deliverer) { s.deliverer = d }

// SetClock 覆盖时钟（测试用）。
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// EnqueueEvent 处理一条 bus 事件：回查快照、匹配端点、为每个匹配端点建 pending 投递行。
// 调用方在 bus 发布方 goroutine 内同步执行——这里只做查询与落库，绝不做 HTTP 投递。
func (s *Service) EnqueueEvent(ctx context.Context, eventName, aggregateID, eventID string) {
	if !IsSupportedEvent(eventName) {
		return
	}
	payload, err := s.buildPayload(ctx, eventName, aggregateID, eventID)
	if err != nil {
		s.logger.WithError(err).WithField("event", eventName).
			Warn("webhook: skip event, cannot build payload")
		return
	}
	endpoints, err := s.repo.ListEndpoints(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("webhook: list endpoints failed")
		return
	}
	for i := range endpoints {
		ep := endpoints[i]
		if !ep.Active || !endpointSubscribes(&ep, eventName) {
			continue
		}
		delivery := &models.WebhookDelivery{
			EndpointID:  ep.ID,
			EventName:   eventName,
			EventID:     eventID,
			AggregateID: aggregateID,
			Status:      models.WebhookDeliveryStatusPending,
			Payload:     payload,
		}
		if err := s.repo.CreateDelivery(ctx, delivery); err != nil {
			s.logger.WithError(err).Warnf("webhook: enqueue delivery for endpoint %d failed", ep.ID)
		}
	}
}

// endpointSubscribes 判断端点是否订阅了该事件。events 为空 = 全部白名单事件。
func endpointSubscribes(ep *models.WebhookEndpoint, eventName string) bool {
	if strings.TrimSpace(ep.Events) == "" {
		return true
	}
	for _, name := range strings.Split(ep.Events, ",") {
		if strings.TrimSpace(name) == eventName {
			return true
		}
	}
	return false
}

// buildPayload 按 AggregateID 前缀回查快照并组装投递 envelope。
// 前缀约定：ticket:<id> / conversation:<uuid> / routing:<sessionID> / voice:<callID>。
func (s *Service) buildPayload(ctx context.Context, eventName, aggregateID, eventID string) (string, error) {
	var data interface{}
	switch {
	case strings.HasPrefix(aggregateID, "ticket:"):
		id, err := strconv.ParseUint(strings.TrimPrefix(aggregateID, "ticket:"), 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid ticket aggregate id %q", aggregateID)
		}
		ticket, err := s.repo.GetTicketSnapshot(ctx, uint(id))
		if err != nil {
			return "", fmt.Errorf("ticket snapshot %d: %w", id, err)
		}
		data = ticket
	case strings.HasPrefix(aggregateID, "conversation:"), strings.HasPrefix(aggregateID, "routing:"):
		sessionID := strings.TrimPrefix(aggregateID, "conversation:")
		sessionID = strings.TrimPrefix(sessionID, "routing:")
		session, err := s.repo.GetSessionSnapshot(ctx, sessionID)
		if err != nil {
			return "", fmt.Errorf("session snapshot %q: %w", sessionID, err)
		}
		data = session
	case strings.HasPrefix(aggregateID, "voice:"):
		callID := strings.TrimPrefix(aggregateID, "voice:")
		call, err := s.repo.GetCallSnapshot(ctx, callID)
		if err != nil {
			return "", fmt.Errorf("call snapshot %q: %w", callID, err)
		}
		data = call
	default:
		return "", fmt.Errorf("unsupported aggregate id %q", aggregateID)
	}
	envelope := map[string]interface{}{
		"id":         eventID,
		"event":      eventName,
		"created_at": s.now().UTC().Format(time.RFC3339),
		"data":       data,
	}
	encoded, err := marshalEnvelope(envelope)
	if err != nil {
		return "", err
	}
	return truncatePayload(encoded), nil
}

func truncatePayload(body []byte) string {
	if len(body) <= payloadTruncateLimit {
		return string(body)
	}
	return string(body[:payloadTruncateLimit])
}

// GenerateSecret 生成端点签名密钥明文（仅创建/轮换时返回一次）。
func GenerateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := readRandom(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// EndpointRequest 端点创建/更新载荷。
type EndpointRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Events      string `json:"events"`
	Description string `json:"description"`
	Active      *bool  `json:"active"`
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
}

func (s *Service) validateEvents(events string) error {
	if strings.TrimSpace(events) == "" {
		return nil
	}
	for _, name := range strings.Split(events, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !IsSupportedEvent(name) {
			return fmt.Errorf("unsupported event: %s", name)
		}
	}
	return nil
}

func validateEndpointURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid url: %s", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https: %s", raw)
	}
	return nil
}

func (s *Service) ListEndpoints(ctx context.Context) ([]models.WebhookEndpoint, error) {
	return s.repo.ListEndpoints(ctx)
}

func (s *Service) GetEndpoint(ctx context.Context, id uint) (*models.WebhookEndpoint, error) {
	return s.repo.GetEndpoint(ctx, id)
}

func (s *Service) CreateEndpoint(ctx context.Context, req EndpointRequest) (*models.WebhookEndpoint, string, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, "", fmt.Errorf("name required")
	}
	if err := validateEndpointURL(req.URL); err != nil {
		return nil, "", err
	}
	if err := s.validateEvents(req.Events); err != nil {
		return nil, "", err
	}
	secret, err := newSecret()
	if err != nil {
		return nil, "", err
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	ep := &models.WebhookEndpoint{
		TenantID:    req.TenantID,
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		URL:         req.URL,
		Secret:      secret,
		Events:      normalizeEvents(req.Events),
		Description: req.Description,
		Active:      active,
	}
	if err := s.repo.CreateEndpoint(ctx, ep); err != nil {
		return nil, "", err
	}
	return ep, secret, nil
}

func (s *Service) UpdateEndpoint(ctx context.Context, id uint, req EndpointRequest) (*models.WebhookEndpoint, error) {
	ep, err := s.repo.GetEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) != "" {
		ep.Name = req.Name
	}
	if req.URL != "" {
		if err := validateEndpointURL(req.URL); err != nil {
			return nil, err
		}
		ep.URL = req.URL
	}
	if req.Events != "" {
		if err := s.validateEvents(req.Events); err != nil {
			return nil, err
		}
		ep.Events = normalizeEvents(req.Events)
	}
	ep.Description = req.Description
	if req.Active != nil {
		ep.Active = *req.Active
	}
	if err := s.repo.UpdateEndpoint(ctx, ep); err != nil {
		return nil, err
	}
	return ep, nil
}

func normalizeEvents(events string) string {
	parts := strings.Split(events, ",")
	kept := make([]string, 0, len(parts))
	for _, name := range parts {
		if name = strings.TrimSpace(name); name != "" {
			kept = append(kept, name)
		}
	}
	return strings.Join(kept, ",")
}

func (s *Service) DeleteEndpoint(ctx context.Context, id uint) error {
	return s.repo.DeleteEndpoint(ctx, id)
}

// RotateEndpointSecret 轮换签名密钥，明文仅本次返回。
func (s *Service) RotateEndpointSecret(ctx context.Context, id uint) (*models.WebhookEndpoint, string, error) {
	ep, err := s.repo.GetEndpoint(ctx, id)
	if err != nil {
		return nil, "", err
	}
	secret, err := newSecret()
	if err != nil {
		return nil, "", err
	}
	ep.Secret = secret
	if err := s.repo.UpdateEndpoint(ctx, ep); err != nil {
		return nil, "", err
	}
	return ep, secret, nil
}

// TestEndpoint 用 ping 事件同步投递一次（不走 worker），结果直接返回给调用方。
func (s *Service) TestEndpoint(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	ep, err := s.repo.GetEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.deliverOnce(ctx, ep, "ping", "", "", map[string]string{"ping": time.Now().UTC().Format(time.RFC3339)}, true)
}

// DeliverOneShot 为 automation call_webhook 动作执行一次性签名投递并落审计行。
// endpoint_id=0 表示非订阅端点来源；失败即 failed 终态（无退避重试），
// 错误由 AutomationRun 记录、投递日志支持手动重放。
func (s *Service) DeliverOneShot(ctx context.Context, url, secret, eventName, aggregateID string, payload map[string]interface{}) error {
	if url == "" {
		return ErrNilRequest
	}
	ep := &models.WebhookEndpoint{URL: url, Secret: secret}
	delivery, err := s.deliverOnce(ctx, ep, eventName, "", aggregateID, payload, true)
	if err != nil {
		return err
	}
	if delivery.Status != models.WebhookDeliveryStatusSuccess {
		if delivery.LastError != "" {
			return fmt.Errorf("webhook delivery failed: %s", delivery.LastError)
		}
		return fmt.Errorf("webhook delivery failed with http status %d", delivery.HTTPStatus)
	}
	return nil
}

// ListDeliveries 分页查询投递日志。
func (s *Service) ListDeliveries(ctx context.Context, query DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return s.repo.ListDeliveries(ctx, query)
}

// RedeliverDelivery 将一条失败/死信投递重置为 pending 立即重投。
func (s *Service) RedeliverDelivery(ctx context.Context, id uint) (*models.WebhookDelivery, error) {
	delivery, err := s.repo.GetDelivery(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ResetDeliveryForRedeliver(ctx, id); err != nil {
		return nil, err
	}
	delivery.Status = models.WebhookDeliveryStatusPending
	delivery.Attempt = 0
	delivery.NextRetryAt = nil
	delivery.LastError = ""
	return delivery, nil
}

// ProcessDueDeliveries 领取到期投递并并发投递，返回处理条数。
func (s *Service) ProcessDueDeliveries(ctx context.Context, now time.Time) int {
	if s.deliverer == nil {
		return 0
	}
	claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	due, err := s.repo.ClaimDueDeliveries(claimCtx, now.UTC(), 50)
	if err != nil {
		s.logger.WithError(err).Warn("webhook: claim due deliveries failed")
		return 0
	}
	if len(due) == 0 {
		return 0
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range due {
		delivery := due[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.processDelivery(ctx, &delivery, now)
		}()
	}
	wg.Wait()
	return len(due)
}

func (s *Service) processDelivery(ctx context.Context, delivery *models.WebhookDelivery, now time.Time) {
	ep, err := s.repo.GetEndpoint(ctx, delivery.EndpointID)
	if err != nil || ep == nil || !ep.Active {
		s.logger.Warnf("webhook: endpoint %d unavailable, mark delivery %d dead", delivery.EndpointID, delivery.ID)
		_ = s.repo.MarkDeliveryFailure(ctx, delivery.ID, delivery.Attempt+1, 0, 0, "endpoint unavailable or inactive", true, nil)
		return
	}
	s.deliverOnceForDelivery(ctx, ep, delivery, now)
}

// deliverOnceForDelivery 处理订阅路径的一条 pending 行。
func (s *Service) deliverOnceForDelivery(ctx context.Context, ep *models.WebhookEndpoint, delivery *models.WebhookDelivery, now time.Time) {
	result := s.deliverer.Deliver(ctx, DeliveryRequest{
		URL:        ep.URL,
		Secret:     ep.Secret,
		EventName:  delivery.EventName,
		EventID:    delivery.EventID,
		DeliveryID: uint64(delivery.ID),
		Body:       []byte(delivery.Payload),
		Timeout:    30 * time.Second,
	})
	if result.Success {
		_ = s.repo.MarkDeliverySuccess(ctx, delivery.ID, result.StatusCode, result.DurationMs, s.now().UTC())
		return
	}
	attempt := delivery.Attempt + 1
	dead := noRetryStatuses[result.StatusCode] || attempt >= maxAttempts
	var nextRetry *time.Time
	if !dead {
		next := now.UTC().Add(retryBackoff[attempt-1])
		nextRetry = &next
	}
	if err := s.repo.MarkDeliveryFailure(ctx, delivery.ID, attempt, result.StatusCode, result.DurationMs, result.Error, dead, nextRetry); err != nil {
		s.logger.WithError(err).Warnf("webhook: record failure for delivery %d failed", delivery.ID)
	}
}

// deliverOnce 一次性同步投递（测试事件 / automation 动作），返回带终态的投递记录。
func (s *Service) deliverOnce(ctx context.Context, ep *models.WebhookEndpoint, eventName, eventID, aggregateID string, data interface{}, persist bool) (*models.WebhookDelivery, error) {
	envelope := map[string]interface{}{
		"id":         eventID,
		"event":      eventName,
		"created_at": s.now().UTC().Format(time.RFC3339),
		"data":       data,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	delivery := &models.WebhookDelivery{
		EndpointID:  ep.ID,
		EventName:   eventName,
		EventID:     eventID,
		AggregateID: aggregateID,
		Status:      models.WebhookDeliveryStatusPending,
		Payload:     truncatePayload(body),
	}
	if s.deliverer == nil {
		return nil, fmt.Errorf("webhook deliverer not configured")
	}
	result := s.deliverer.Deliver(ctx, DeliveryRequest{
		URL:       ep.URL,
		Secret:    ep.Secret,
		EventName: eventName,
		EventID:   eventID,
		Body:      body,
		Timeout:   15 * time.Second,
	})
	now := s.now().UTC()
	delivery.Attempt = 1
	delivery.HTTPStatus = result.StatusCode
	delivery.DurationMs = result.DurationMs
	delivery.LastError = result.Error
	if result.Success {
		delivery.Status = models.WebhookDeliveryStatusSuccess
		delivery.DeliveredAt = &now
	} else {
		delivery.Status = models.WebhookDeliveryStatusFailed
	}
	if persist {
		if err := s.repo.CreateDelivery(ctx, delivery); err != nil {
			return delivery, err
		}
	}
	return delivery, nil
}
