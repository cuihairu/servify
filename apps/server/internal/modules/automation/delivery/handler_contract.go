package delivery

import (
	"context"
	automationdomain "servify/apps/server/internal/modules/automation/domain"

	automationapp "servify/apps/server/internal/modules/automation/application"
)

type TriggerRequest = automationapp.TriggerRequest
type RunListQuery = automationapp.RunListQuery
type BatchRunRequest = automationapp.BatchRunRequest
type BatchRunResponse = automationapp.BatchRunResponse

// HandlerService is the only automation contract that HTTP handlers should depend on.
type HandlerService interface {
	ListTriggers(ctx context.Context) ([]automationdomain.AutomationTrigger, error)
	CreateTrigger(ctx context.Context, req *TriggerRequest) (*automationdomain.AutomationTrigger, error)
	DeleteTrigger(ctx context.Context, id uint) error
	ListRuns(ctx context.Context, req *RunListQuery) ([]automationdomain.AutomationRun, int64, error)
	BatchRun(ctx context.Context, req *BatchRunRequest) (*BatchRunResponse, error)
}
