package application

// 端口级单测：stubRepo 字段即返回值，覆盖服务层全部分支（GORM 语义由 infra 侧
// db 型测试对账；本文件只验证服务逻辑与错误文案）。文案断言与 handlers 的字符串
// 映射（isNotFoundError/isConflictError/isInvalidInputError）联动，改动须同步。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdomain "servify/apps/server/internal/modules/automation/domain"
)

var errBoom = errors.New("boom")

func uintPtr(v uint) *uint { return &v }
func boolPtr(v bool) *bool { return &v }

// stubRepo 内存实现 Repository：字段即返回值，零值即零值返回。
type stubRepo struct {
	// sla_configs
	createdConfig             *models.SLAConfig // CreateConfig 捕获（含调用方填的字段）
	createConfigErr           error
	saveConfigErr             error
	config                    *models.SLAConfig
	configErr                 error
	deleteConfigRows          int64
	deleteConfigErr           error
	countConfigsFilteredTotal int64
	countConfigsFilteredErr   error
	listConfigs               []models.SLAConfig
	listConfigsErr            error
	findPriorityTier          *models.SLAConfig
	findPriorityTierErr       error
	findPriorityExcept        *models.SLAConfig
	findPriorityExceptErr     error
	findPriorityTierExcept    *models.SLAConfig
	findPriorityTierExceptErr error
	findActiveTier            *models.SLAConfig
	findActiveTierErr         error
	findDefaultActive         *models.SLAConfig
	findDefaultActiveErr      error
	countConfigsTotal         int64
	countConfigsErr           error
	countActiveTotal          int64
	countActiveErr            error

	// tickets
	ticket                  *models.Ticket
	ticketErr               error
	countTicketsTotal       int64
	countTicketsErr         error
	countTicketsByDateTotal int64
	countTicketsByDateErr   error
	activeTickets           []models.Ticket
	activeTicketsErr        error

	// customers
	customerPriority    string
	customerPriorityErr error

	// sla_violations
	createdViolation             *models.SLAViolation
	createViolationErr           error
	violation                    *models.SLAViolation
	violationErr                 error
	existingViolation            *models.SLAViolation
	existingViolationErr         error
	countViolationsFilteredTotal int64
	countViolationsFilteredErr   error
	listViolations               []models.SLAViolation
	listViolationsErr            error
	resolveViolationRows         int64
	resolveViolationErr          error
	resolveByTicketRows          int64
	resolveByTicketErr           error
	countViolationsTotal         int64
	countViolationsErr           error
	countByConfigTotal           int64
	countByConfigErr             error
	countUnresolvedTotal         int64
	countUnresolvedErr           error
	countViolationsByDateTotal   int64
	countViolationsByDateErr     error
	byType                       map[string]int
	byTypeErr                    error
	byPriority                   map[string]int
	byPriorityErr                error
	byTier                       map[string]int
	byTierErr                    error
}

var _ Repository = (*stubRepo)(nil)

func (r *stubRepo) CreateConfig(ctx context.Context, config *models.SLAConfig) error {
	r.createdConfig = config
	return r.createConfigErr
}
func (r *stubRepo) SaveConfig(ctx context.Context, config *models.SLAConfig) error {
	return r.saveConfigErr
}
func (r *stubRepo) GetConfigScoped(ctx context.Context, id uint) (*models.SLAConfig, error) {
	return r.config, r.configErr
}
func (r *stubRepo) DeleteConfig(ctx context.Context, id uint) (int64, error) {
	return r.deleteConfigRows, r.deleteConfigErr
}
func (r *stubRepo) CountConfigsFiltered(ctx context.Context, req *SLAConfigListRequest) (int64, error) {
	return r.countConfigsFilteredTotal, r.countConfigsFilteredErr
}
func (r *stubRepo) ListConfigs(ctx context.Context, req *SLAConfigListRequest) ([]models.SLAConfig, error) {
	return r.listConfigs, r.listConfigsErr
}
func (r *stubRepo) FindConfigByPriorityTier(ctx context.Context, priority, tier string) (*models.SLAConfig, error) {
	return r.findPriorityTier, r.findPriorityTierErr
}
func (r *stubRepo) FindConfigByPriorityExceptID(ctx context.Context, priority string, excludeID uint) (*models.SLAConfig, error) {
	return r.findPriorityExcept, r.findPriorityExceptErr
}
func (r *stubRepo) FindConfigByPriorityTierExceptID(ctx context.Context, priority, tier string, excludeID uint) (*models.SLAConfig, error) {
	return r.findPriorityTierExcept, r.findPriorityTierExceptErr
}
func (r *stubRepo) FindConfigByPriorityTierActive(ctx context.Context, priority, tier string) (*models.SLAConfig, error) {
	return r.findActiveTier, r.findActiveTierErr
}
func (r *stubRepo) FindDefaultConfigByPriorityActive(ctx context.Context, priority string) (*models.SLAConfig, error) {
	return r.findDefaultActive, r.findDefaultActiveErr
}
func (r *stubRepo) CountConfigs(ctx context.Context) (int64, error) {
	return r.countConfigsTotal, r.countConfigsErr
}
func (r *stubRepo) CountActiveConfigs(ctx context.Context) (int64, error) {
	return r.countActiveTotal, r.countActiveErr
}

func (r *stubRepo) GetTicketScoped(ctx context.Context, id uint) (*models.Ticket, error) {
	return r.ticket, r.ticketErr
}
func (r *stubRepo) CountTickets(ctx context.Context) (int64, error) {
	return r.countTicketsTotal, r.countTicketsErr
}
func (r *stubRepo) CountTicketsByDate(ctx context.Context, date string) (int64, error) {
	return r.countTicketsByDateTotal, r.countTicketsByDateErr
}
func (r *stubRepo) ListActiveTickets(ctx context.Context) ([]models.Ticket, error) {
	return r.activeTickets, r.activeTicketsErr
}
func (r *stubRepo) GetCustomerPriorityScoped(ctx context.Context, userID uint) (string, error) {
	return r.customerPriority, r.customerPriorityErr
}

func (r *stubRepo) CreateViolation(ctx context.Context, violation *models.SLAViolation) error {
	r.createdViolation = violation
	return r.createViolationErr
}
func (r *stubRepo) GetViolationScoped(ctx context.Context, id uint) (*models.SLAViolation, error) {
	return r.violation, r.violationErr
}
func (r *stubRepo) FindViolationByTicketConfigType(ctx context.Context, ticketID, configID uint, violationType string) (*models.SLAViolation, error) {
	return r.existingViolation, r.existingViolationErr
}
func (r *stubRepo) CountViolationsFiltered(ctx context.Context, req *SLAViolationListRequest) (int64, error) {
	return r.countViolationsFilteredTotal, r.countViolationsFilteredErr
}
func (r *stubRepo) ListViolations(ctx context.Context, req *SLAViolationListRequest) ([]models.SLAViolation, error) {
	return r.listViolations, r.listViolationsErr
}
func (r *stubRepo) ResolveViolation(ctx context.Context, id uint) (int64, error) {
	return r.resolveViolationRows, r.resolveViolationErr
}
func (r *stubRepo) ResolveViolationsByTicket(ctx context.Context, ticketID uint, violationTypes []string) (int64, error) {
	return r.resolveByTicketRows, r.resolveByTicketErr
}
func (r *stubRepo) CountViolations(ctx context.Context) (int64, error) {
	return r.countViolationsTotal, r.countViolationsErr
}
func (r *stubRepo) CountViolationsByConfig(ctx context.Context, configID uint) (int64, error) {
	return r.countByConfigTotal, r.countByConfigErr
}
func (r *stubRepo) CountUnresolvedViolations(ctx context.Context) (int64, error) {
	return r.countUnresolvedTotal, r.countUnresolvedErr
}
func (r *stubRepo) CountViolationsByDate(ctx context.Context, date string) (int64, error) {
	return r.countViolationsByDateTotal, r.countViolationsByDateErr
}
func (r *stubRepo) ViolationsByType(ctx context.Context) (map[string]int, error) {
	return r.byType, r.byTypeErr
}
func (r *stubRepo) ViolationsByPriority(ctx context.Context) (map[string]int, error) {
	return r.byPriority, r.byPriorityErr
}
func (r *stubRepo) ViolationsByTier(ctx context.Context) (map[string]int, error) {
	return r.byTier, r.byTierErr
}

// automationNoopRepo 让 HandleEvent 在触发器查询处安全返回（嵌入接口满足编译，
// 未实现方法一旦被调用即 panic——本测试路径只走 ListActiveTriggersByEvent）。
type automationNoopRepo struct {
	automationapp.Repository
}

func (automationNoopRepo) ListActiveTriggersByEvent(ctx context.Context, event string) ([]automationdomain.AutomationTrigger, error) {
	return nil, nil
}

// lateTicket 构造一个必触发 first_response 违约的工单。
func lateTicket(id uint) *models.Ticket {
	return &models.Ticket{
		ID: id, Title: "T", Priority: "high", Status: "open",
		CreatedAt: time.Now().Add(-2 * time.Hour), UpdatedAt: time.Now(),
	}
}

func TestSLAServiceConstructors(t *testing.T) {
	svc := NewService(&stubRepo{}, nil)
	if svc == nil {
		t.Fatal("nil service")
	}
	automation := automationapp.NewService(nil)
	svc.SetAutomationModule(automation)
	svc.SetAutomationModule(nil) // 幂等复位
}

func TestCreateSLAConfigStub(t *testing.T) {
	ctx := context.Background()
	base := &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}

	if _, err := NewService(&stubRepo{}, nil).CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "bogus", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	}); err == nil || !strings.Contains(err.Error(), "invalid priority") {
		t.Fatalf("invalid priority: %v", err)
	}
	if _, err := NewService(&stubRepo{}, nil).CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 60, ResolutionTime: 60, EscalationTime: 30,
	}); err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("first >= resolution: %v", err)
	}
	if _, err := NewService(&stubRepo{}, nil).CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 60,
	}); err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("escalation >= resolution: %v", err)
	}

	dup := &stubRepo{findPriorityTier: &models.SLAConfig{ID: 9}}
	if _, err := NewService(dup, nil).CreateSLAConfig(ctx, base); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dup: %v", err)
	}

	checkErr := &stubRepo{findPriorityTierErr: errBoom}
	if _, err := NewService(checkErr, nil).CreateSLAConfig(ctx, base); err == nil || !strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("check err: %v", err)
	}

	createErr := &stubRepo{createConfigErr: errBoom}
	if _, err := NewService(createErr, nil).CreateSLAConfig(ctx, base); err == nil || !strings.Contains(err.Error(), "failed to create SLA config") {
		t.Fatalf("create err: %v", err)
	}

	// 默认 Active=true、tier 归一、tags 拼接
	ok := &stubRepo{}
	config, err := NewService(ok, nil).CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", CustomerTier: "  VIP ", Tags: []string{" a ", "", "b"},
		FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
	})
	if err != nil || config == nil {
		t.Fatalf("ok: %v %+v", config, err)
	}
	if !config.Active || config.CustomerTier != "vip" || config.Tags != "a,b" || config.WarningThreshold != 80 {
		t.Fatalf("defaults: %+v", config)
	}
	if ok.createdConfig != config {
		t.Fatal("expected repo to receive same config")
	}

	inactive := &stubRepo{}
	config, err = NewService(inactive, nil).CreateSLAConfig(ctx, &SLAConfigCreateRequest{
		Name: "A", Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30,
		Active: boolPtr(false),
	})
	if err != nil || config.Active {
		t.Fatalf("inactive default: %+v %v", config, err)
	}
}

func TestGetSLAConfigStub(t *testing.T) {
	ctx := context.Background()

	notFound := &stubRepo{configErr: ErrSLAConfigNotFound}
	if _, err := NewService(notFound, nil).GetSLAConfig(ctx, 1); err == nil || err.Error() != "SLA config not found" {
		t.Fatalf("not found: %v", err)
	}

	rawErr := &stubRepo{configErr: errBoom}
	if _, err := NewService(rawErr, nil).GetSLAConfig(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to get SLA config") {
		t.Fatalf("raw err: %v", err)
	}

	ok := &stubRepo{config: &models.SLAConfig{ID: 3}}
	config, err := NewService(ok, nil).GetSLAConfig(ctx, 3)
	if err != nil || config == nil || config.ID != 3 {
		t.Fatalf("ok: %+v %v", config, err)
	}
}

func TestListSLAConfigsStub(t *testing.T) {
	ctx := context.Background()

	countErr := &stubRepo{countConfigsFilteredErr: errBoom}
	if _, _, err := NewService(countErr, nil).ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10}); err == nil || !strings.Contains(err.Error(), "failed to count SLA configs") {
		t.Fatalf("count err: %v", err)
	}

	listErr := &stubRepo{listConfigsErr: errBoom}
	if _, _, err := NewService(listErr, nil).ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10}); err == nil || !strings.Contains(err.Error(), "failed to list SLA configs") {
		t.Fatalf("list err: %v", err)
	}

	ok := &stubRepo{countConfigsFilteredTotal: 2, listConfigs: []models.SLAConfig{{ID: 1}, {ID: 2}}}
	configs, total, err := NewService(ok, nil).ListSLAConfigs(ctx, &SLAConfigListRequest{Page: 1, PageSize: 10})
	if err != nil || total != 2 || len(configs) != 2 {
		t.Fatalf("ok: %d %+v %v", total, configs, err)
	}
}

func TestUpdateSLAConfigStub(t *testing.T) {
	ctx := context.Background()
	// 服务就地改写加载到的配置对象，逐用例取新实例避免串扰
	mkCfg := func() *models.SLAConfig {
		return &models.SLAConfig{ID: 1, Name: "Old", Priority: "high", CustomerTier: "", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30}
	}

	notFound := &stubRepo{configErr: ErrSLAConfigNotFound}
	if _, err := NewService(notFound, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{}); err == nil || err.Error() != "SLA config not found" {
		t.Fatalf("not found: %v", err)
	}

	findErr := &stubRepo{configErr: errBoom}
	if _, err := NewService(findErr, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{}); err == nil || !strings.Contains(err.Error(), "failed to find SLA config") {
		t.Fatalf("find err: %v", err)
	}

	svc := NewService(&stubRepo{config: mkCfg()}, nil)
	if _, err := svc.UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{Priority: stringPtrSLA("bogus")}); err == nil || !strings.Contains(err.Error(), "invalid priority") {
		t.Fatalf("invalid priority: %v", err)
	}

	prioDup := &stubRepo{config: mkCfg(), findPriorityExcept: &models.SLAConfig{ID: 2}}
	if _, err := NewService(prioDup, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{Priority: stringPtrSLA("low")}); err == nil || !strings.Contains(err.Error(), "SLA config for priority 'low' already exists") {
		t.Fatalf("priority dup: %v", err)
	}

	prioErr := &stubRepo{config: mkCfg(), findPriorityExceptErr: errBoom}
	if _, err := NewService(prioErr, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{Priority: stringPtrSLA("low")}); err == nil || !strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("priority check err: %v", err)
	}

	if _, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{FirstResponseTime: intPtr(0)}); err == nil || !strings.Contains(err.Error(), "must be at least") {
		t.Fatalf("first < 1: %v", err)
	}
	if _, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{ResolutionTime: intPtr(0)}); err == nil || !strings.Contains(err.Error(), "must be at least") {
		t.Fatalf("resolution < 1: %v", err)
	}
	if _, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{EscalationTime: intPtr(0)}); err == nil || !strings.Contains(err.Error(), "must be at least") {
		t.Fatalf("escalation < 1: %v", err)
	}
	if _, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{FirstResponseTime: intPtr(60)}); err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("first >= resolution: %v", err)
	}
	if _, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{EscalationTime: intPtr(60)}); err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("escalation >= resolution: %v", err)
	}

	tierDup := &stubRepo{config: mkCfg(), findPriorityTierExcept: &models.SLAConfig{ID: 2}}
	if _, err := NewService(tierDup, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{Name: stringPtrSLA("New")}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("tier dup: %v", err)
	}

	tierErr := &stubRepo{config: mkCfg(), findPriorityTierExceptErr: errBoom}
	if _, err := NewService(tierErr, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{}); err == nil || !strings.Contains(err.Error(), "failed to check existing config") {
		t.Fatalf("tier check err: %v", err)
	}

	saveErr := &stubRepo{config: mkCfg(), saveConfigErr: errBoom}
	if _, err := NewService(saveErr, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{}); err == nil || !strings.Contains(err.Error(), "failed to update SLA config") {
		t.Fatalf("save err: %v", err)
	}

	updated, err := NewService(&stubRepo{config: mkCfg()}, nil).UpdateSLAConfig(ctx, 1, &SLAConfigUpdateRequest{
		Name: stringPtrSLA("New"), Priority: stringPtrSLA("low"), CustomerTier: stringPtrSLA("  VIP "), Tags: []string{"x"},
		WarningThreshold: intPtr(90), ResolutionTime: intPtr(90), BusinessHoursOnly: boolPtr(true), Active: boolPtr(false),
	})
	if err != nil || updated.Name != "New" || updated.CustomerTier != "vip" || updated.Tags != "x" ||
		updated.WarningThreshold != 90 || !updated.BusinessHoursOnly || updated.Active {
		t.Fatalf("ok: %+v %v", updated, err)
	}
}

func TestDeleteSLAConfigStub(t *testing.T) {
	ctx := context.Background()

	countErr := &stubRepo{countByConfigErr: errBoom}
	if err := NewService(countErr, nil).DeleteSLAConfig(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to check SLA violations") {
		t.Fatalf("count err: %v", err)
	}

	hasViolations := &stubRepo{countByConfigTotal: 3}
	if err := NewService(hasViolations, nil).DeleteSLAConfig(ctx, 1); err == nil || !strings.Contains(err.Error(), "cannot delete SLA config") {
		t.Fatalf("has violations: %v", err)
	}

	deleteErr := &stubRepo{deleteConfigErr: errBoom}
	if err := NewService(deleteErr, nil).DeleteSLAConfig(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to delete SLA config") {
		t.Fatalf("delete err: %v", err)
	}

	zeroRows := &stubRepo{deleteConfigRows: 0}
	if err := NewService(zeroRows, nil).DeleteSLAConfig(ctx, 1); err == nil || err.Error() != "SLA config not found" {
		t.Fatalf("zero rows: %v", err)
	}

	if err := NewService(&stubRepo{deleteConfigRows: 1}, nil).DeleteSLAConfig(ctx, 1); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

func TestGetSLAConfigByPriorityStub(t *testing.T) {
	ctx := context.Background()

	tierErr := &stubRepo{findActiveTierErr: errBoom}
	if _, err := NewService(tierErr, nil).GetSLAConfigByPriority(ctx, "high", "vip"); err == nil || !strings.Contains(err.Error(), "failed to get SLA config by priority and tier") {
		t.Fatalf("tier err: %v", err)
	}

	tierHit := &stubRepo{findActiveTier: &models.SLAConfig{ID: 7}}
	config, err := NewService(tierHit, nil).GetSLAConfigByPriority(ctx, "high", "  VIP ")
	if err != nil || config == nil || config.ID != 7 {
		t.Fatalf("tier hit: %+v %v", config, err)
	}

	defaultErr := &stubRepo{findDefaultActiveErr: errBoom}
	if _, err := NewService(defaultErr, nil).GetSLAConfigByPriority(ctx, "high", "vip"); err == nil || !strings.Contains(err.Error(), "failed to get SLA config by priority") {
		t.Fatalf("default err: %v", err)
	}

	miss := &stubRepo{}
	if config, err := NewService(miss, nil).GetSLAConfigByPriority(ctx, "high", ""); err != nil || config != nil {
		t.Fatalf("miss: %+v %v", config, err)
	}

	defaultHit := &stubRepo{findDefaultActive: &models.SLAConfig{ID: 8}}
	config, err = NewService(defaultHit, nil).GetSLAConfigByPriority(ctx, "high", "")
	if err != nil || config == nil || config.ID != 8 {
		t.Fatalf("default hit: %+v %v", config, err)
	}
}

func TestCheckSLAViolationStub(t *testing.T) {
	ctx := context.Background()
	cfg := &models.SLAConfig{ID: 5, Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30, Active: true}

	svc := NewService(&stubRepo{}, nil)
	if _, err := svc.CheckSLAViolation(ctx, nil); err == nil || !strings.Contains(err.Error(), "ticket required") {
		t.Fatalf("nil ticket: %v", err)
	}

	notFound := &stubRepo{ticketErr: ErrTicketNotFound}
	if _, err := NewService(notFound, nil).CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err == nil || err.Error() != "ticket not found" {
		t.Fatalf("ticket not found: %v", err)
	}

	ticketErr := &stubRepo{ticketErr: errBoom}
	if _, err := NewService(ticketErr, nil).CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to validate ticket scope") {
		t.Fatalf("ticket err: %v", err)
	}

	// 无 SLA 配置 -> (nil, nil)
	noCfg := &stubRepo{ticket: &models.Ticket{ID: 1, Priority: "low", Status: "open", CreatedAt: time.Now()}}
	if v, err := NewService(noCfg, nil).CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err != nil || v != nil {
		t.Fatalf("no config: %+v %v", v, err)
	}

	// 配置查询失败
	cfgErr := &stubRepo{ticket: &models.Ticket{ID: 1, Priority: "high", Status: "open", CreatedAt: time.Now()}, findDefaultActiveErr: errBoom}
	if _, err := NewService(cfgErr, nil).CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to get SLA config") {
		t.Fatalf("config err: %v", err)
	}

	// 未违约（新工单）
	fresh := &stubRepo{
		ticket:            &models.Ticket{ID: 1, Priority: "high", Status: "open", CreatedAt: time.Now()},
		findDefaultActive: cfg,
	}
	if v, err := NewService(fresh, nil).CheckSLAViolation(ctx, &models.Ticket{ID: 1}); err != nil || v != nil {
		t.Fatalf("fresh: %+v %v", v, err)
	}

	late := lateTicket(1)

	// 同类型违约已存在 -> 返回既有记录
	dup := &stubRepo{ticket: late, findDefaultActive: cfg, existingViolation: &models.SLAViolation{ID: 77}}
	if v, err := NewService(dup, nil).CheckSLAViolation(ctx, late); err != nil || v == nil || v.ID != 77 {
		t.Fatalf("existing: %+v %v", v, err)
	}

	existingErr := &stubRepo{ticket: late, findDefaultActive: cfg, existingViolationErr: errBoom}
	if _, err := NewService(existingErr, nil).CheckSLAViolation(ctx, late); err == nil || !strings.Contains(err.Error(), "failed to check existing violation") {
		t.Fatalf("existing err: %v", err)
	}

	createErr := &stubRepo{ticket: late, findDefaultActive: cfg, createViolationErr: errBoom}
	if _, err := NewService(createErr, nil).CheckSLAViolation(ctx, late); err == nil || !strings.Contains(err.Error(), "failed to create SLA violation") {
		t.Fatalf("create err: %v", err)
	}

	reloadErr := &stubRepo{ticket: late, findDefaultActive: cfg, violationErr: errBoom}
	if _, err := NewService(reloadErr, nil).CheckSLAViolation(ctx, late); err == nil || !strings.Contains(err.Error(), "failed to load created SLA violation") {
		t.Fatalf("reload err: %v", err)
	}

	// 全链路 ok（含自动化工单触发分支）
	ok := &stubRepo{ticket: late, findDefaultActive: cfg, violation: &models.SLAViolation{ID: 9, ViolationType: "first_response"}}
	svc = NewService(ok, nil)
	svc.SetAutomationModule(automationapp.NewService(automationNoopRepo{}))
	v, err := svc.CheckSLAViolation(ctx, late)
	if err != nil || v == nil || v.ID != 9 {
		t.Fatalf("ok: %+v %v", v, err)
	}
	if ok.createdViolation == nil || ok.createdViolation.ViolationType != "first_response" {
		t.Fatalf("created violation: %+v", ok.createdViolation)
	}
}

func TestCreateSLAViolationStub(t *testing.T) {
	ctx := context.Background()
	ticket := &models.Ticket{ID: 3, TenantID: "t1", WorkspaceID: "w1", Title: "T"}

	if err := NewService(&stubRepo{}, nil).CreateSLAViolation(ctx, nil); err == nil || !strings.Contains(err.Error(), "violation required") {
		t.Fatalf("nil: %v", err)
	}

	notFound := &stubRepo{ticketErr: ErrTicketNotFound}
	if err := NewService(notFound, nil).CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 1}); err == nil || err.Error() != "ticket not found" {
		t.Fatalf("ticket not found: %v", err)
	}

	ticketErr := &stubRepo{ticketErr: errBoom}
	if err := NewService(ticketErr, nil).CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 1}); err == nil || !strings.Contains(err.Error(), "failed to validate violation ticket") {
		t.Fatalf("ticket err: %v", err)
	}

	cfgNotFound := &stubRepo{ticket: ticket, configErr: ErrSLAConfigNotFound}
	if err := NewService(cfgNotFound, nil).CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 3}); err == nil || err.Error() != "sla config not found" {
		t.Fatalf("config not found (lowercase): %v", err)
	}

	cfgErr := &stubRepo{ticket: ticket, configErr: errBoom}
	if err := NewService(cfgErr, nil).CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 3}); err == nil || !strings.Contains(err.Error(), "failed to validate violation config") {
		t.Fatalf("config err: %v", err)
	}

	createErr := &stubRepo{ticket: ticket, createViolationErr: errBoom}
	if err := NewService(createErr, nil).CreateSLAViolation(ctx, &models.SLAViolation{TicketID: 3}); err == nil || !strings.Contains(err.Error(), "failed to create SLA violation") {
		t.Fatalf("create err: %v", err)
	}

	ok := &stubRepo{ticket: ticket}
	violation := &models.SLAViolation{TicketID: 3, SLAConfigID: 5, ViolationType: "first_response"}
	if err := NewService(ok, nil).CreateSLAViolation(ctx, violation); err != nil {
		t.Fatalf("ok: %v", err)
	}
	// 租户/工作空间随工单盖章
	if ok.createdViolation.TenantID != "t1" || ok.createdViolation.WorkspaceID != "w1" || ok.createdViolation.CreatedAt.IsZero() {
		t.Fatalf("stamped: %+v", ok.createdViolation)
	}
}

func TestListSLAViolationsStub(t *testing.T) {
	ctx := context.Background()

	countErr := &stubRepo{countViolationsFilteredErr: errBoom}
	if _, _, err := NewService(countErr, nil).ListSLAViolations(ctx, &SLAViolationListRequest{Page: 1, PageSize: 10}); err == nil || !strings.Contains(err.Error(), "failed to count SLA violations") {
		t.Fatalf("count err: %v", err)
	}

	listErr := &stubRepo{listViolationsErr: errBoom}
	if _, _, err := NewService(listErr, nil).ListSLAViolations(ctx, &SLAViolationListRequest{Page: 1, PageSize: 10}); err == nil || !strings.Contains(err.Error(), "failed to list SLA violations") {
		t.Fatalf("list err: %v", err)
	}

	ok := &stubRepo{countViolationsFilteredTotal: 1, listViolations: []models.SLAViolation{{ID: 1}}}
	items, total, err := NewService(ok, nil).ListSLAViolations(ctx, &SLAViolationListRequest{Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("ok: %d %+v %v", total, items, err)
	}
}

func TestResolveSLAViolationStub(t *testing.T) {
	ctx := context.Background()

	rawErr := &stubRepo{resolveViolationErr: errBoom}
	if err := NewService(rawErr, nil).ResolveSLAViolation(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to resolve SLA violation") {
		t.Fatalf("raw err: %v", err)
	}

	zeroRows := &stubRepo{resolveViolationRows: 0}
	if err := NewService(zeroRows, nil).ResolveSLAViolation(ctx, 1); err == nil || err.Error() != "SLA violation not found" {
		t.Fatalf("zero rows: %v", err)
	}

	if err := NewService(&stubRepo{resolveViolationRows: 1}, nil).ResolveSLAViolation(ctx, 1); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

func TestResolveViolationsByTicketStub(t *testing.T) {
	ctx := context.Background()

	notFound := &stubRepo{ticketErr: ErrTicketNotFound}
	if err := NewService(notFound, nil).ResolveViolationsByTicket(ctx, 1, nil); err == nil || err.Error() != "ticket not found" {
		t.Fatalf("not found: %v", err)
	}

	ticketErr := &stubRepo{ticketErr: errBoom}
	if err := NewService(ticketErr, nil).ResolveViolationsByTicket(ctx, 1, nil); err == nil || !strings.Contains(err.Error(), "failed to validate ticket") {
		t.Fatalf("ticket err: %v", err)
	}

	updateErr := &stubRepo{ticket: &models.Ticket{ID: 1}, resolveByTicketErr: errBoom}
	if err := NewService(updateErr, nil).ResolveViolationsByTicket(ctx, 1, nil); err == nil || !strings.Contains(err.Error(), "failed to resolve SLA violations") {
		t.Fatalf("update err: %v", err)
	}

	// 0 行也不算错（批量操作语义）
	noop := &stubRepo{ticket: &models.Ticket{ID: 1}, resolveByTicketRows: 0}
	if err := NewService(noop, nil).ResolveViolationsByTicket(ctx, 1, []string{"first_response"}); err != nil {
		t.Fatalf("noop: %v", err)
	}

	if err := NewService(&stubRepo{ticket: &models.Ticket{ID: 1}, resolveByTicketRows: 2}, nil).ResolveViolationsByTicket(ctx, 1, nil); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

func TestGetSLAStatsStub(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		repo       *stubRepo
		wantSubstr string
	}{
		{&stubRepo{countConfigsErr: errBoom}, "failed to count SLA configs"},
		{&stubRepo{countActiveErr: errBoom}, "failed to count active SLA configs"},
		{&stubRepo{countViolationsErr: errBoom}, "failed to count total violations"},
		{&stubRepo{countUnresolvedErr: errBoom}, "failed to count unresolved violations"},
		{&stubRepo{byTypeErr: errBoom}, "failed to get violation type stats"},
		{&stubRepo{byPriorityErr: errBoom}, "failed to get violation priority stats"},
		{&stubRepo{countTicketsErr: errBoom}, "failed to count total tickets"},
	}
	for _, tc := range cases {
		if _, err := NewService(tc.repo, nil).GetSLAStats(ctx); err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("expected %q, got %v", tc.wantSubstr, err)
		}
	}

	// tier 统计失败沿用旧行为：静默跳过（不返回错误）
	tierFail := &stubRepo{byTierErr: errBoom}
	if stats, err := NewService(tierFail, nil).GetSLAStats(ctx); err != nil || stats == nil {
		t.Fatalf("tier fail swallowed: %+v %v", stats, err)
	}

	// 趋势失败只记日志，TrendData 保持空
	trendFail := &stubRepo{countTicketsByDateErr: errBoom}
	if stats, err := NewService(trendFail, nil).GetSLAStats(ctx); err != nil || len(stats.TrendData) != 0 {
		t.Fatalf("trend fail swallowed: %+v %v", stats, err)
	}
	violationTrendFail := &stubRepo{countViolationsByDateErr: errBoom}
	if stats, err := NewService(violationTrendFail, nil).GetSLAStats(ctx); err != nil || len(stats.TrendData) != 0 {
		t.Fatalf("violation trend fail swallowed: %+v %v", stats, err)
	}

	// 全绿：合规率与分布
	ok := &stubRepo{
		countConfigsTotal:          3,
		countActiveTotal:           2,
		countViolationsTotal:       1,
		countUnresolvedTotal:       1,
		byType:                     map[string]int{"first_response": 1},
		byPriority:                 map[string]int{"high": 1},
		byTier:                     map[string]int{"vip": 1},
		countTicketsTotal:          4,
		countTicketsByDateTotal:    4,
		countViolationsByDateTotal: 1,
	}
	stats, err := NewService(ok, nil).GetSLAStats(ctx)
	if err != nil {
		t.Fatalf("ok: %v", err)
	}
	if stats.TotalConfigs != 3 || stats.ActiveConfigs != 2 || stats.TotalViolations != 1 || stats.UnresolvedViolations != 1 {
		t.Fatalf("counts: %+v", stats)
	}
	if stats.ViolationsByType["first_response"] != 1 || stats.ViolationsByPriority["high"] != 1 || stats.ViolationsByTier["vip"] != 1 {
		t.Fatalf("distribution: %+v", stats)
	}
	if stats.ComplianceRate != 75 {
		t.Fatalf("compliance rate: %+v", stats)
	}
	if len(stats.TrendData) != 7 {
		t.Fatalf("trend days: %d", len(stats.TrendData))
	}
	if stats.TrendData[6].ComplianceRate != 75 {
		t.Fatalf("trend rate: %+v", stats.TrendData[6])
	}

	// 零工单 -> 合规率 100
	zeroTickets := &stubRepo{}
	stats, err = NewService(zeroTickets, nil).GetSLAStats(ctx)
	if err != nil || stats.ComplianceRate != 100 {
		t.Fatalf("zero tickets: %+v %v", stats, err)
	}
}

func TestMonitorStub(t *testing.T) {
	ctx := context.Background()

	listErr := &stubRepo{activeTicketsErr: errBoom}
	if err := NewService(listErr, nil).RunMonitorOnce(ctx); err == nil || !strings.Contains(err.Error(), "failed to get active tickets") {
		t.Fatalf("list err: %v", err)
	}

	// 单工单检查失败：记日志继续，返回 nil
	perTicketErr := &stubRepo{
		activeTickets: []models.Ticket{{ID: 1}},
		ticketErr:     errBoom,
	}
	if err := NewService(perTicketErr, nil).RunMonitorOnce(ctx); err != nil {
		t.Fatalf("per-ticket err swallowed: %v", err)
	}

	// 正常扫描：无配置 -> 无违约
	cfg := &models.SLAConfig{ID: 5, Priority: "high", FirstResponseTime: 5, ResolutionTime: 60, EscalationTime: 30, Active: true}
	late := lateTicket(1)
	found := &stubRepo{
		activeTickets:     []models.Ticket{*late},
		ticket:            late,
		findDefaultActive: cfg,
		violation:         &models.SLAViolation{ID: 9},
	}
	if err := NewService(found, nil).RunMonitorOnce(ctx); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

func TestSLA_TrendDataErrorsStub(t *testing.T) {
	ctx := context.Background()

	ticketFail := &stubRepo{countTicketsByDateErr: errBoom}
	if _, err := NewService(ticketFail, nil).getSLATrendData(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to count tickets for date") {
		t.Fatalf("ticket trend err: %v", err)
	}

	violationFail := &stubRepo{countViolationsByDateErr: errBoom}
	if _, err := NewService(violationFail, nil).getSLATrendData(ctx, 1); err == nil || !strings.Contains(err.Error(), "failed to count violations for date") {
		t.Fatalf("violation trend err: %v", err)
	}
}

func stringPtrSLA(v string) *string { return &v }
