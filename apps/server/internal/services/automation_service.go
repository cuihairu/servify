package services

import (
	"context"
	"fmt"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	automationinfra "servify/apps/server/internal/modules/automation/infra"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AutomationEvent represents an event that can trigger automations.
type AutomationEvent = automationapp.Event

// TriggerCondition describes a single condition entry.
type TriggerCondition = automationapp.TriggerCondition

// TriggerAction describes an action to execute when trigger matches.
type TriggerAction = automationapp.TriggerAction

// AutomationTriggerRequest 创建触发器的请求
type AutomationTriggerRequest = automationapp.TriggerRequest

// AutomationRunListRequest 查询执行记录
type AutomationRunListRequest = automationapp.RunListQuery

// AutomationBatchRunRequest 批量执行请求
type AutomationBatchRunRequest = automationapp.BatchRunRequest

type AutomationBatchRunTicketResult = automationapp.BatchRunTicketResult

type AutomationBatchRunResponse = automationapp.BatchRunResponse

// AutomationService handles trigger evaluation and action execution.
type AutomationService struct {
	db         *gorm.DB
	logger     *logrus.Logger
	module     *automationapp.Service
	subscriber *automationdelivery.EventBusSubscriber
}

func NewAutomationService(db *gorm.DB, logger *logrus.Logger) *AutomationService {
	if logger == nil {
		logger = logrus.New()
	}
	repo := automationinfra.NewGormRepository(db)
	module := automationapp.NewService(repo)
	return &AutomationService{
		db:         db,
		logger:     logger,
		module:     module,
		subscriber: automationdelivery.NewEventBusSubscriber(module),
	}
}

// TimersForWorker 暴露 delay 执行单处理器给 timer worker。
// 返回门面持有的 module 实例（事件订阅与 webhook dispatcher 都在它上面），
// 而不是 delivery.HandlerServiceAdapter 背后的另一个实例。
func (s *AutomationService) TimersForWorker() automationapp.TimerProcessor {
	if s == nil || s.module == nil {
		return nil
	}
	return s.module
}

// SetTimerBatchSize 覆盖单轮 timer 扫描的执行单上限。
func (s *AutomationService) SetTimerBatchSize(n int) {
	if s == nil || s.module == nil {
		return
	}
	s.module.SetTimerBatchSize(n)
}

func (s *AutomationService) SetEventBus(bus eventbus.Bus) {
	if s.subscriber != nil {
		s.subscriber.Register(bus)
	}
}

// SetWebhookDispatcher 注入 call_webhook 动作的外呼实现（由 webhook 模块提供）。
func (s *AutomationService) SetWebhookDispatcher(d automationapp.WebhookDispatcher) {
	s.module.SetWebhookDispatcher(d)
}

func (s *AutomationService) HandleEvent(ctx context.Context, evt AutomationEvent) {
	s.module.HandleEvent(ctx, evt)
}

func (s *AutomationService) ListRuns(ctx context.Context, req *AutomationRunListRequest) ([]models.AutomationRun, int64, error) {
	query := automationapp.RunListQuery{}
	if req != nil {
		query = *req
	}
	return s.module.ListRuns(ctx, query)
}

func (s *AutomationService) BatchRun(ctx context.Context, req *AutomationBatchRunRequest) (*AutomationBatchRunResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	return s.module.BatchRun(ctx, *req)
}

func (s *AutomationService) ListTriggers(ctx context.Context) ([]models.AutomationTrigger, error) {
	return s.module.ListTriggers(ctx)
}

func (s *AutomationService) CreateTrigger(ctx context.Context, req *AutomationTriggerRequest) (*models.AutomationTrigger, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	return s.module.CreateTrigger(ctx, *req)
}

func (s *AutomationService) DeleteTrigger(ctx context.Context, id uint) error {
	return s.module.DeleteTrigger(ctx, id)
}

// matchTrigger preserves the legacy test-facing helper while delegating to the automation module.
func (s *AutomationService) matchTrigger(ctx context.Context, trig models.AutomationTrigger, evt AutomationEvent, ticket *models.Ticket, dryRun bool) bool {
	if s == nil || s.module == nil {
		return false
	}

	var ticketView *automationapp.TicketView
	if ticket != nil {
		ticketView = &automationapp.TicketView{
			ID:       ticket.ID,
			Priority: ticket.Priority,
			Status:   ticket.Status,
			Tags:     ticket.Tags,
		}
	}

	return s.module.MatchTrigger(ctx, trig, evt, ticketView, dryRun)
}

func evaluateCondition(cond TriggerCondition, attrs map[string]interface{}) bool {
	return automationapp.EvaluateCondition(cond, attrs)
}

func isSupportedEvent(event string) bool {
	return automationapp.IsSupportedEvent(event)
}
