package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"time"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
	gamificationcontract "servify/apps/server/internal/modules/gamification/contract"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
	routingcontract "servify/apps/server/internal/modules/routing/contract"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	realtimeplatform "servify/apps/server/internal/platform/realtime"
	"servify/apps/server/internal/platform/storage"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

// ---- agent ----

type unitAgentService struct {
	agent      *models.Agent
	agents     []models.Agent
	online     []*agentdelivery.AgentInfo
	stats      *agentdelivery.AgentStats
	info       *agentdelivery.AgentInfo
	createErr  error
	getErr     error
	listErr    error
	onlineErr  error
	statusErr  error
	revokeVer  int
	revokeErr  error
	assignErr  error
	releaseErr error
	findErr    error
	statsErr   error
}

func (s *unitAgentService) CreateAgent(ctx context.Context, req *agentdelivery.AgentCreateRequest) (*models.Agent, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.agent, nil
}

func (s *unitAgentService) GetAgentByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.agent, nil
}

func (s *unitAgentService) ListAgents(ctx context.Context, limit int) ([]models.Agent, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.agents, nil
}

func (s *unitAgentService) AgentGoOnline(ctx context.Context, userID uint) error  { return s.statusErr }
func (s *unitAgentService) AgentGoOffline(ctx context.Context, userID uint) error { return s.statusErr }
func (s *unitAgentService) UpdateAgentStatus(ctx context.Context, userID uint, status string) error {
	return s.statusErr
}

func (s *unitAgentService) RevokeAgentTokens(ctx context.Context, userID uint) (int, error) {
	return s.revokeVer, s.revokeErr
}

func (s *unitAgentService) AssignSessionToAgent(ctx context.Context, sessionID string, agentID uint) error {
	return s.assignErr
}

func (s *unitAgentService) ReleaseSessionFromAgent(ctx context.Context, sessionID string, agentID uint) error {
	return s.releaseErr
}

func (s *unitAgentService) FindAvailableAgent(ctx context.Context, skills []string, priority string) (*agentdelivery.AgentInfo, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.info, nil
}

func (s *unitAgentService) GetOnlineAgents(ctx context.Context) []*agentdelivery.AgentInfo {
	return s.online
}

func (s *unitAgentService) GetAgentStats(ctx context.Context, agentID *uint) (*agentdelivery.AgentStats, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

// ---- customer ----

type unitCustomerService struct {
	customer    *models.User
	customers   []services.CustomerInfo
	activity    *services.CustomerActivity
	stats       *services.CustomerStats
	revokeVer   int
	createErr   error
	getErr      error
	updateErr   error
	listErr     error
	activityErr error
	noteErr     error
	tagsErr     error
	statsErr    error
	revokeErr   error
}

func (s *unitCustomerService) CreateCustomer(ctx context.Context, req *services.CustomerCreateRequest) (*models.User, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.customer, nil
}

func (s *unitCustomerService) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.customer, nil
}

func (s *unitCustomerService) UpdateCustomer(ctx context.Context, customerID uint, req *services.CustomerUpdateRequest) (*models.User, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.customer, nil
}

func (s *unitCustomerService) ListCustomers(ctx context.Context, req *services.CustomerListRequest) ([]services.CustomerInfo, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.customers, int64(len(s.customers)), nil
}

func (s *unitCustomerService) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*services.CustomerActivity, error) {
	if s.activityErr != nil {
		return nil, s.activityErr
	}
	return s.activity, nil
}

func (s *unitCustomerService) AddCustomerNote(ctx context.Context, customerID uint, note string, userID uint) error {
	return s.noteErr
}

func (s *unitCustomerService) UpdateCustomerTags(ctx context.Context, customerID uint, tags []string) error {
	return s.tagsErr
}

func (s *unitCustomerService) GetCustomerStats(ctx context.Context) (*services.CustomerStats, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

func (s *unitCustomerService) RevokeCustomerTokens(ctx context.Context, customerID uint) (int, error) {
	return s.revokeVer, s.revokeErr
}

// ---- ticket ----

type unitTicketService struct {
	ticket     *models.Ticket
	tickets    []models.Ticket
	fields     []models.CustomField
	total      int64
	comment    *models.TicketComment
	bulkResult *ticketcontract.BulkUpdateResult
	sessions   []models.Session
	stats      *ticketcontract.TicketStats
	createErr  error
	getErr     error
	updateErr  error
	listErr    error
	fieldsErr  error
	assignErr  error
	commentErr error
	closeErr   error
	statsErr   error
	bulkErr    error
}

func (s *unitTicketService) CreateTicket(ctx context.Context, req *ticketcontract.CreateTicketRequest) (*models.Ticket, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.ticket, nil
}

func (s *unitTicketService) GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.ticket, nil
}

func (s *unitTicketService) UpdateTicket(ctx context.Context, ticketID uint, req *ticketcontract.UpdateTicketRequest, userID uint) (*models.Ticket, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.ticket, nil
}

func (s *unitTicketService) ListTickets(ctx context.Context, req *ticketcontract.ListTicketRequest) ([]models.Ticket, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.tickets, s.total, nil
}

func (s *unitTicketService) ListTicketCustomFields(ctx context.Context, activeOnly bool) ([]models.CustomField, error) {
	if s.fieldsErr != nil {
		return nil, s.fieldsErr
	}
	return s.fields, nil
}

func (s *unitTicketService) AssignTicket(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
	return s.assignErr
}

func (s *unitTicketService) AddComment(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
	if s.commentErr != nil {
		return nil, s.commentErr
	}
	return s.comment, nil
}

func (s *unitTicketService) CloseTicket(ctx context.Context, ticketID uint, userID uint, reason string) error {
	return s.closeErr
}

func (s *unitTicketService) GetTicketStats(ctx context.Context, agentID *uint) (*ticketcontract.TicketStats, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

func (s *unitTicketService) BulkUpdateTickets(ctx context.Context, req *ticketcontract.BulkUpdateTicketRequest, userID uint) (*ticketcontract.BulkUpdateResult, error) {
	if s.bulkErr != nil {
		return nil, s.bulkErr
	}
	return s.bulkResult, nil
}

func (s *unitTicketService) GetRelatedConversations(ctx context.Context, ticketID uint) ([]models.Session, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.sessions, nil
}

// ---- knowledge docs ----

type unitKnowledgeService struct {
	docs      []models.KnowledgeDoc
	total     int64
	doc       *models.KnowledgeDoc
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func (s *unitKnowledgeService) List(ctx context.Context, req *services.KnowledgeDocListRequest) ([]models.KnowledgeDoc, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.docs, s.total, nil
}

func (s *unitKnowledgeService) Get(ctx context.Context, id uint) (*models.KnowledgeDoc, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.doc, nil
}

func (s *unitKnowledgeService) Create(ctx context.Context, req *services.KnowledgeDocCreateRequest) (*models.KnowledgeDoc, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.doc, nil
}

func (s *unitKnowledgeService) Update(ctx context.Context, id uint, req *services.KnowledgeDocUpdateRequest) (*models.KnowledgeDoc, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.doc, nil
}

func (s *unitKnowledgeService) Delete(ctx context.Context, id uint) error { return s.deleteErr }

// ---- analytics ----

type unitAnalyticsService struct {
	dashboard    *analyticscontract.DashboardStats
	timeRange    []analyticscontract.TimeRangeStats
	agentPerf    []analyticscontract.AgentPerformanceStats
	category     []analyticscontract.CategoryStats
	remoteAssist *analyticscontract.RemoteAssistTicketStats
	dashboardErr error
	timeRangeErr error
	agentPerfErr error
	categoryErr  error
	sourceErr    error
	remoteErr    error
	updateErr    error
}

func (s *unitAnalyticsService) GetDashboardStats(ctx context.Context) (*analyticscontract.DashboardStats, error) {
	if s.dashboardErr != nil {
		return nil, s.dashboardErr
	}
	return s.dashboard, nil
}

func (s *unitAnalyticsService) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.TimeRangeStats, error) {
	if s.timeRangeErr != nil {
		return nil, s.timeRangeErr
	}
	return s.timeRange, nil
}

func (s *unitAnalyticsService) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticscontract.AgentPerformanceStats, error) {
	if s.agentPerfErr != nil {
		return nil, s.agentPerfErr
	}
	return s.agentPerf, nil
}

func (s *unitAnalyticsService) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	return s.category, nil
}

func (s *unitAnalyticsService) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	return s.category, nil
}

func (s *unitAnalyticsService) GetCustomerSourceStats(ctx context.Context) ([]analyticscontract.CategoryStats, error) {
	if s.sourceErr != nil {
		return nil, s.sourceErr
	}
	return s.category, nil
}

func (s *unitAnalyticsService) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	if s.remoteErr != nil {
		return nil, s.remoteErr
	}
	return s.remoteAssist, nil
}

func (s *unitAnalyticsService) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return s.updateErr
}

// ---- routing / session transfer ----

type unitTransferService struct {
	transfer   *routingcontract.TransferResult
	history    []models.TransferRecord
	waiting    []models.WaitingRecord
	cancelErr  error
	processErr error
	shouldTran bool
	humanErr   error
	agentErr   error
	historyErr error
	listErr    error
	waitingErr error
}

func (s *unitTransferService) TransferToHuman(ctx context.Context, req *routingcontract.TransferRequest) (*routingcontract.TransferResult, error) {
	if s.humanErr != nil {
		return nil, s.humanErr
	}
	return s.transfer, nil
}

func (s *unitTransferService) TransferToAgent(ctx context.Context, sessionID string, targetAgentID uint, reason string) (*routingcontract.TransferResult, error) {
	if s.agentErr != nil {
		return nil, s.agentErr
	}
	return s.transfer, nil
}

func (s *unitTransferService) GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return s.history, nil
}

func (s *unitTransferService) ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.history, nil
}

func (s *unitTransferService) ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
	if s.waitingErr != nil {
		return nil, s.waitingErr
	}
	return s.waiting, nil
}

func (s *unitTransferService) CancelWaitingRecord(ctx context.Context, sessionID string, operatorID uint, reason string) error {
	return s.cancelErr
}

func (s *unitTransferService) ProcessWaitingQueue(ctx context.Context) (int, error) {
	if s.processErr != nil {
		return 0, s.processErr
	}
	return 0, nil
}

func (s *unitTransferService) AutoTransferCheck(ctx context.Context, sessionID string, messages []models.Message) bool {
	return s.shouldTran
}

// ---- automation ----

type unitAutomationService struct {
	triggers   []models.AutomationTrigger
	trigger    *models.AutomationTrigger
	runs       []models.AutomationRun
	runTotal   int64
	batch      *automationdelivery.BatchRunResponse
	listErr    error
	createErr  error
	deleteErr  error
	runsErr    error
	batchErr   error
	deleteFail error
}

func (s *unitAutomationService) ListTriggers(ctx context.Context) ([]models.AutomationTrigger, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.triggers, nil
}

func (s *unitAutomationService) CreateTrigger(ctx context.Context, req *automationdelivery.TriggerRequest) (*models.AutomationTrigger, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.trigger, nil
}

func (s *unitAutomationService) DeleteTrigger(ctx context.Context, id uint) error {
	if s.deleteFail != nil {
		return s.deleteFail
	}
	return s.deleteErr
}

func (s *unitAutomationService) ListRuns(ctx context.Context, req *automationdelivery.RunListQuery) ([]models.AutomationRun, int64, error) {
	if s.runsErr != nil {
		return nil, 0, s.runsErr
	}
	return s.runs, s.runTotal, nil
}

func (s *unitAutomationService) BatchRun(ctx context.Context, req *automationdelivery.BatchRunRequest) (*automationdelivery.BatchRunResponse, error) {
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	return s.batch, nil
}

// ---- gamification ----

type unitGamificationService struct {
	resp *gamificationcontract.LeaderboardResponse
	err  error
}

func (s *unitGamificationService) GetLeaderboard(ctx context.Context, req *gamificationdelivery.LeaderboardRequest) (*gamificationcontract.LeaderboardResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

// ---- suggestion ----

type unitSuggestionService struct {
	resp *suggestioncontract.SuggestionResponse
	err  error
}

func (s *unitSuggestionService) Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

// ---- satisfaction ----

type unitSatisfactionService struct {
	sat         *models.CustomerSatisfaction
	sats        []models.CustomerSatisfaction
	surveys     []models.SatisfactionSurvey
	survey      *models.SatisfactionSurvey
	preview     *services.SatisfactionSurveyPreview
	stats       *services.SatisfactionStatsResponse
	total       int64
	createErr   error
	getErr      error
	listErr     error
	surveyErr   error
	resendErr   error
	byTicketErr error
	statsErr    error
	updateErr   error
	deleteErr   error
	previewErr  error
	respondErr  error
}

func (s *unitSatisfactionService) CreateSatisfaction(ctx context.Context, req *services.SatisfactionCreateRequest) (*models.CustomerSatisfaction, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.sat, nil
}

func (s *unitSatisfactionService) GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.sat, nil
}

func (s *unitSatisfactionService) ListSatisfactions(ctx context.Context, req *services.SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.sats, s.total, nil
}

func (s *unitSatisfactionService) ListSurveys(ctx context.Context, req *services.SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error) {
	if s.surveyErr != nil {
		return nil, 0, s.surveyErr
	}
	return s.surveys, s.total, nil
}

func (s *unitSatisfactionService) ResendSurvey(ctx context.Context, id uint) (*models.SatisfactionSurvey, error) {
	if s.resendErr != nil {
		return nil, s.resendErr
	}
	return s.survey, nil
}

func (s *unitSatisfactionService) GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error) {
	if s.byTicketErr != nil {
		return nil, s.byTicketErr
	}
	return s.sat, nil
}

func (s *unitSatisfactionService) GetSatisfactionStats(ctx context.Context, dateFrom, dateTo *time.Time) (*services.SatisfactionStatsResponse, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

func (s *unitSatisfactionService) UpdateSatisfaction(ctx context.Context, id uint, comment string) (*models.CustomerSatisfaction, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.sat, nil
}

func (s *unitSatisfactionService) DeleteSatisfaction(ctx context.Context, id uint) error {
	return s.deleteErr
}

func (s *unitSatisfactionService) GetSurveyPreviewByToken(ctx context.Context, token string) (*services.SatisfactionSurveyPreview, error) {
	if s.previewErr != nil {
		return nil, s.previewErr
	}
	return s.preview, nil
}

func (s *unitSatisfactionService) RespondSurvey(ctx context.Context, token string, rating int, comment string) (*models.CustomerSatisfaction, error) {
	if s.respondErr != nil {
		return nil, s.respondErr
	}
	return s.sat, nil
}

// ---- SLA ----

type unitSLAService struct {
	config       *models.SLAConfig
	configs      []models.SLAConfig
	violation    *models.SLAViolation
	violations   []models.SLAViolation
	stats        *services.SLAStatsResponse
	total        int64
	createErr    error
	getErr       error
	listErr      error
	updateErr    error
	deleteErr    error
	priorityErr  error
	violationErr error
	resolveErr   error
	statsErr     error
	checkErr     error
}

func (s *unitSLAService) CreateSLAConfig(ctx context.Context, req *services.SLAConfigCreateRequest) (*models.SLAConfig, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.config, nil
}

func (s *unitSLAService) GetSLAConfig(ctx context.Context, id uint) (*models.SLAConfig, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.config, nil
}

func (s *unitSLAService) ListSLAConfigs(ctx context.Context, req *services.SLAConfigListRequest) ([]models.SLAConfig, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.configs, s.total, nil
}

func (s *unitSLAService) UpdateSLAConfig(ctx context.Context, id uint, req *services.SLAConfigUpdateRequest) (*models.SLAConfig, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.config, nil
}

func (s *unitSLAService) DeleteSLAConfig(ctx context.Context, id uint) error { return s.deleteErr }

func (s *unitSLAService) GetSLAConfigByPriority(ctx context.Context, priority string, customerTier string) (*models.SLAConfig, error) {
	if s.priorityErr != nil {
		return nil, s.priorityErr
	}
	return s.config, nil
}

func (s *unitSLAService) ListSLAViolations(ctx context.Context, req *services.SLAViolationListRequest) ([]models.SLAViolation, int64, error) {
	if s.violationErr != nil {
		return nil, 0, s.violationErr
	}
	return s.violations, s.total, nil
}

func (s *unitSLAService) ResolveSLAViolation(ctx context.Context, id uint) error { return s.resolveErr }

func (s *unitSLAService) GetSLAStats(ctx context.Context) (*services.SLAStatsResponse, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

func (s *unitSLAService) CheckSLAViolation(ctx context.Context, ticket *models.Ticket) (*models.SLAViolation, error) {
	if s.checkErr != nil {
		return nil, s.checkErr
	}
	return s.violation, nil
}

// ---- macro ----

type unitMacroService struct {
	macros    []models.Macro
	macro     *models.Macro
	comment   *models.TicketComment
	listErr   error
	createErr error
	updateErr error
	deleteErr error
	applyErr  error
}

func (s *unitMacroService) List(ctx context.Context) ([]models.Macro, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.macros, nil
}

func (s *unitMacroService) Create(ctx context.Context, req *services.MacroCreateRequest) (*models.Macro, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.macro, nil
}

func (s *unitMacroService) Update(ctx context.Context, id uint, req *services.MacroUpdateRequest) (*models.Macro, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.macro, nil
}

func (s *unitMacroService) Delete(ctx context.Context, id uint) error { return s.deleteErr }

func (s *unitMacroService) ApplyToTicket(ctx context.Context, macroID, ticketID, actorID uint) (*models.TicketComment, error) {
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	return s.comment, nil
}

// ---- shift ----

type unitShiftService struct {
	shift     *models.ShiftSchedule
	shifts    []models.ShiftSchedule
	stats     *services.ShiftStatsResponse
	total     int64
	createErr error
	listErr   error
	updateErr error
	deleteErr error
	statsErr  error
}

func (s *unitShiftService) CreateShift(ctx context.Context, req *services.ShiftCreateRequest) (*models.ShiftSchedule, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.shift, nil
}

func (s *unitShiftService) ListShifts(ctx context.Context, req *services.ShiftListRequest) ([]models.ShiftSchedule, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.shifts, s.total, nil
}

func (s *unitShiftService) UpdateShift(ctx context.Context, id uint, req *services.ShiftUpdateRequest) (*models.ShiftSchedule, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.shift, nil
}

func (s *unitShiftService) DeleteShift(ctx context.Context, id uint) error { return s.deleteErr }

func (s *unitShiftService) GetShiftStats(ctx context.Context) (*services.ShiftStatsResponse, error) {
	if s.statsErr != nil {
		return nil, s.statsErr
	}
	return s.stats, nil
}

// ---- app market ----

type unitAppMarketService struct {
	items     []*services.AppIntegration
	item      *services.AppIntegration
	total     int64
	listErr   error
	createErr error
	updateErr error
	deleteErr error
}

func (s *unitAppMarketService) List(ctx context.Context, req *services.AppIntegrationListRequest) ([]*services.AppIntegration, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.items, s.total, nil
}

func (s *unitAppMarketService) Create(ctx context.Context, req *services.AppIntegrationCreateRequest) (*services.AppIntegration, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.item, nil
}

func (s *unitAppMarketService) Update(ctx context.Context, id uint, req *services.AppIntegrationUpdateRequest) (*services.AppIntegration, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.item, nil
}

func (s *unitAppMarketService) Delete(ctx context.Context, id uint) error { return s.deleteErr }

// ---- custom field ----

type unitCustomFieldService struct {
	fields    []models.CustomField
	field     *models.CustomField
	listErr   error
	getErr    error
	createErr error
	updateErr error
	deleteErr error
}

func (s *unitCustomFieldService) List(ctx context.Context, resource string, activeOnly bool) ([]models.CustomField, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.fields, nil
}

func (s *unitCustomFieldService) Get(ctx context.Context, id uint) (*models.CustomField, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.field, nil
}

func (s *unitCustomFieldService) Create(ctx context.Context, req *services.CustomFieldCreateRequest) (*models.CustomField, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.field, nil
}

func (s *unitCustomFieldService) Update(ctx context.Context, id uint, req *services.CustomFieldUpdateRequest) (*models.CustomField, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.field, nil
}

func (s *unitCustomFieldService) Delete(ctx context.Context, id uint) error { return s.deleteErr }

// ---- ai ----

type unitAIService struct {
	queryResp interface{}
	status    map[string]interface{}
	metrics   *services.AIMetrics
	metricsOK bool
	uploadErr error
	syncErr   error
	enableOK  bool
	resetOK   bool
}

func (s *unitAIService) ProcessQuery(ctx context.Context, query string, sessionID string) (interface{}, error) {
	if s.uploadErr != nil && s.queryResp == nil {
		return nil, s.uploadErr
	}
	return s.queryResp, nil
}

func (s *unitAIService) GetStatus(ctx context.Context) map[string]interface{} { return s.status }

func (s *unitAIService) GetMetrics() (*services.AIMetrics, bool) { return s.metrics, s.metricsOK }

func (s *unitAIService) UploadKnowledgeDocument(ctx context.Context, title, content string, tags []string) error {
	return s.uploadErr
}

func (s *unitAIService) SyncKnowledgeBase(ctx context.Context) error { return s.syncErr }

func (s *unitAIService) SetKnowledgeProviderEnabled(enabled bool) bool { return s.enableOK }

func (s *unitAIService) ResetCircuitBreaker() bool { return s.resetOK }

// ---- storage ----

type unitStorageProvider struct {
	info    *storage.ObjectInfo
	err     error
	lastKey string
}

func (p *unitStorageProvider) Save(key string, r io.Reader, size int64) (*storage.ObjectInfo, error) {
	p.lastKey = key
	if p.err != nil {
		return nil, p.err
	}
	_, _ = io.Copy(io.Discard, r)
	if p.info != nil {
		return p.info, nil
	}
	return &storage.ObjectInfo{Key: key, Size: size, URL: "/files/" + key}, nil
}

func (p *unitStorageProvider) Open(key string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (p *unitStorageProvider) Delete(key string) error { return nil }
func (p *unitStorageProvider) PresignedURL(key string, expiresSeconds int) (string, error) {
	return "", nil
}

// ---- workspace overview ----

type unitWorkspaceReader struct {
	overview  *services.WorkspaceOverview
	err       error
	lastLimit int
}

func (r *unitWorkspaceReader) GetOverview(ctx context.Context, limit int) (*services.WorkspaceOverview, error) {
	r.lastLimit = limit
	if r.err != nil {
		return nil, r.err
	}
	return r.overview, nil
}

// ---- websocket gateway ----

type unitWsGateway struct {
	handled int
	count   int
}

func (g *unitWsGateway) HandleWebSocket(c *gin.Context) { g.handled++ }
func (g *unitWsGateway) SendToSession(sessionID string, message realtimeplatform.Message) {
}
func (g *unitWsGateway) ClientCount() int { return g.count }

// ---- database ping ----

type unitDBPing struct {
	db  *sql.DB
	err error
}

func (p *unitDBPing) DB() (*sql.DB, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.db, nil
}

// ---- misc helpers ----

type unitNoopRTCGateway struct{}

func (unitNoopRTCGateway) ConnectionStats(sessionID string) (map[string]interface{}, error) {
	return map[string]interface{}{"session_id": sessionID}, nil
}
func (unitNoopRTCGateway) ConnectionCount() int { return 2 }
func (unitNoopRTCGateway) HandleOffer(string, webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	return nil, nil
}
func (unitNoopRTCGateway) HandleAnswer(string, webrtc.SessionDescription) error { return nil }
func (unitNoopRTCGateway) HandleICECandidate(string, webrtc.ICECandidateInit) error {
	return nil
}
func (unitNoopRTCGateway) CloseConnection(string) error { return nil }

func transferResultFixture() *routingcontract.TransferResult {
	now := time.Now()
	return &routingcontract.TransferResult{
		Success:       true,
		SessionID:     "s1",
		NewAgentID:    2,
		TransferredAt: now,
		Summary:       "transferred",
	}
}

func transferHistoryFixture() []models.TransferRecord {
	now := time.Now()
	agentID := uint(2)
	return []models.TransferRecord{
		{
			ID:            1,
			SessionID:     "s1",
			FromAgentID:   &agentID,
			ToAgentID:     &agentID,
			Reason:        "load_balance",
			TransferredAt: now,
			CreatedAt:     now,
		},
	}
}
