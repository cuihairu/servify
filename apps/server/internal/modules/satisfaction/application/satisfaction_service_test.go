package application

// 应用层逻辑测试（写归属下沉 infra 后，服务与仓储解耦）：内存仓储桩 + 按方法
// 注入返回/错误，覆盖业务分支与错误文案；SQL 落库错误分支在 infra 包测。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// stubRepo 内存实现 Repository：字段即返回值，零值即零值返回。
type stubRepo struct {
	ticket            *models.Ticket
	ticketErr         error
	ticketForAgent    *models.Ticket
	ticketForAgentErr error
	customer          *models.Customer
	customerErr       error
	agent             *models.Agent
	agentErr          error
	user              *models.User
	userErr           error
	userEmailOK       bool

	countByTicket    int64
	countByTicketErr error
	findExisting     *models.CustomerSatisfaction
	findExistingErr  error

	surveyByToken       *models.SatisfactionSurvey
	surveyByTokenErr    error
	surveyScoped        *models.SatisfactionSurvey
	surveyScopedErr     error
	surveyActive        *models.SatisfactionSurvey
	surveyActiveErr     error
	surveys             []models.SatisfactionSurvey
	surveysTotal        int64
	surveysErr          error
	createSurveyErr     error
	saveSurveyErr       error
	expireSurveyErr     error
	expireSurveyCalls   int
	completeSurveyErr   error
	completeSurveyCalls int

	expireOverdueRows int64
	expireOverdueErr  error
	pendingSurveys    []models.SatisfactionSurvey
	pendingErr        error
	markFailedErr     error
	markSentErr       error
	markSentRows      int64

	createSatisfactionErr   error
	satisfaction            *models.CustomerSatisfaction
	satisfactionErr         error
	findSatisfaction        *models.CustomerSatisfaction
	findSatisfactionErr     error
	satisfactionByTicket    *models.CustomerSatisfaction
	satisfactionByTicketErr error
	saveSatisfactionErr     error
	satisfactions           []models.CustomerSatisfaction
	satisfactionsTotal      int64
	satisfactionsErr        error
	deleteSatisfactionRows  int64
	deleteSatisfactionErr   error

	statsCount    int64
	statsCountErr error
	statsAvg      float64
	statsAvgErr   error
	statsDist     map[int]int
	statsDistErr  error
	statsCat      map[string]SatisfactionStat
	statsCatErr   error
	statsTrend    []SatisfactionTrend
	statsTrendErr error
}

var _ Repository = (*stubRepo)(nil)

func (r *stubRepo) GetTicketScoped(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return r.ticket, r.ticketErr
}
func (r *stubRepo) GetTicketWithAgentScoped(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return r.ticketForAgent, r.ticketForAgentErr
}
func (r *stubRepo) GetCustomerScopedByUserID(ctx context.Context, userID uint) (*models.Customer, error) {
	return r.customer, r.customerErr
}
func (r *stubRepo) GetAgentScopedByUserID(ctx context.Context, userID uint) (*models.Agent, error) {
	return r.agent, r.agentErr
}
func (r *stubRepo) GetUserEmail(ctx context.Context, userID uint) (string, bool) {
	if r.userErr != nil {
		return "", false
	}
	if r.user == nil || !r.userEmailOK {
		return "", false
	}
	return r.user.Email, true
}
func (r *stubRepo) CountSatisfactionsByTicket(ctx context.Context, ticketID uint) (int64, error) {
	return r.countByTicket, r.countByTicketErr
}
func (r *stubRepo) FindSatisfactionByTicketAndCustomer(ctx context.Context, ticketID, customerID uint) (*models.CustomerSatisfaction, error) {
	return r.findExisting, r.findExistingErr
}
func (r *stubRepo) FindActiveSurveyByTicket(ctx context.Context, ticketID uint) (*models.SatisfactionSurvey, error) {
	return r.surveyActive, r.surveyActiveErr
}
func (r *stubRepo) CreateSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error {
	return r.createSurveyErr
}
func (r *stubRepo) SaveSurvey(ctx context.Context, survey *models.SatisfactionSurvey) error {
	return r.saveSurveyErr
}
func (r *stubRepo) ListSurveys(ctx context.Context, req *SatisfactionSurveyListRequest) ([]models.SatisfactionSurvey, int64, error) {
	return r.surveys, r.surveysTotal, r.surveysErr
}
func (r *stubRepo) GetSurveyByToken(ctx context.Context, token string) (*models.SatisfactionSurvey, error) {
	return r.surveyByToken, r.surveyByTokenErr
}
func (r *stubRepo) GetSurveyScoped(ctx context.Context, id uint) (*models.SatisfactionSurvey, error) {
	return r.surveyScoped, r.surveyScopedErr
}
func (r *stubRepo) ExpireSurvey(ctx context.Context, surveyID uint) error {
	r.expireSurveyCalls++
	return r.expireSurveyErr
}
func (r *stubRepo) CompleteSurvey(ctx context.Context, surveyID uint, completedAt time.Time, satisfactionID uint) error {
	r.completeSurveyCalls++
	return r.completeSurveyErr
}
func (r *stubRepo) ExpireOverdueQueuedSurveys(ctx context.Context, now time.Time) (int64, error) {
	return r.expireOverdueRows, r.expireOverdueErr
}
func (r *stubRepo) ListPendingEmailSurveys(ctx context.Context, now time.Time, batchSize int) ([]models.SatisfactionSurvey, error) {
	return r.pendingSurveys, r.pendingErr
}
func (r *stubRepo) MarkSurveyFailedIfQueued(ctx context.Context, surveyID uint) error {
	return r.markFailedErr
}
func (r *stubRepo) MarkSurveySentIfQueued(ctx context.Context, surveyID uint, sentAt time.Time) (int64, error) {
	return r.markSentRows, r.markSentErr
}
func (r *stubRepo) CreateSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error {
	return r.createSatisfactionErr
}
func (r *stubRepo) GetSatisfaction(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	return r.satisfaction, r.satisfactionErr
}
func (r *stubRepo) FindSatisfactionScoped(ctx context.Context, id uint) (*models.CustomerSatisfaction, error) {
	return r.findSatisfaction, r.findSatisfactionErr
}
func (r *stubRepo) GetSatisfactionByTicket(ctx context.Context, ticketID uint) (*models.CustomerSatisfaction, error) {
	return r.satisfactionByTicket, r.satisfactionByTicketErr
}
func (r *stubRepo) ListSatisfactions(ctx context.Context, req *SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error) {
	return r.satisfactions, r.satisfactionsTotal, r.satisfactionsErr
}
func (r *stubRepo) DeleteSatisfaction(ctx context.Context, id uint) (int64, error) {
	return r.deleteSatisfactionRows, r.deleteSatisfactionErr
}
func (r *stubRepo) SaveSatisfaction(ctx context.Context, satisfaction *models.CustomerSatisfaction) error {
	return r.saveSatisfactionErr
}
func (r *stubRepo) CountSatisfactions(ctx context.Context, dateFrom, dateTo *time.Time) (int64, error) {
	return r.statsCount, r.statsCountErr
}
func (r *stubRepo) AverageSatisfactionRating(ctx context.Context, dateFrom, dateTo *time.Time) (float64, error) {
	return r.statsAvg, r.statsAvgErr
}
func (r *stubRepo) SatisfactionRatingDistribution(ctx context.Context, dateFrom, dateTo *time.Time) (map[int]int, error) {
	return r.statsDist, r.statsDistErr
}
func (r *stubRepo) SatisfactionCategoryStats(ctx context.Context, dateFrom, dateTo *time.Time) (map[string]SatisfactionStat, error) {
	return r.statsCat, r.statsCatErr
}
func (r *stubRepo) SatisfactionTrend(ctx context.Context, dateFrom, dateTo *time.Time) ([]SatisfactionTrend, error) {
	return r.statsTrend, r.statsTrendErr
}

func newStubService(repo Repository) *SatisfactionService {
	svc := NewService(repo, nil)
	svc.SetSurveyMailer(&stubMailer{})
	svc.SetSurveyLinkBaseURL("https://support.example.com/")
	return svc
}

// stubMailer 空投递：记录调用、可注入错误。
type stubMailer struct {
	calls int
	err   error
}

func (m *stubMailer) SendSurveyEmail(ctx context.Context, to, subject, textBody string) error {
	m.calls++
	return m.err
}

var errBoom = errors.New("boom")

func TestSatisfactionServiceConstructors(t *testing.T) {
	svc := NewService(&stubRepo{}, nil)
	if svc == nil {
		t.Fatal("NewService nil")
	}
	// setter 空跑覆盖（装配层注入链）
	svc.SetSurveyMailer(&stubMailer{})
	svc.SetSurveyLinkBaseURL(" https://x/ ")
}

func TestScheduleSurveyStub(t *testing.T) {
	svc := newStubService(&stubRepo{})
	if _, err := svc.ScheduleSurvey(context.Background(), nil); err == nil {
		t.Fatal("expected nil ticket error")
	}

	notFound := &stubRepo{}
	notFound.ticketErr = ErrTicketNotFound
	if _, err := newStubService(notFound).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err == nil || err.Error() != "ticket not found" {
		t.Fatalf("not found: %v", err)
	}

	ticketErr := &stubRepo{}
	ticketErr.ticketErr = errBoom
	if _, err := newStubService(ticketErr).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to validate ticket for survey") {
		t.Fatalf("ticket err: %v", err)
	}

	countErr := &stubRepo{ticket: &models.Ticket{ID: 1}, countByTicketErr: errBoom}
	if _, err := newStubService(countErr).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to check satisfaction status") {
		t.Fatalf("count err: %v", err)
	}

	already := &stubRepo{ticket: &models.Ticket{ID: 1}, countByTicket: 1}
	if survey, err := newStubService(already).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err != nil || survey != nil {
		t.Fatalf("existing satisfaction must suppress: %v %v", survey, err)
	}

	activeErr := &stubRepo{ticket: &models.Ticket{ID: 1}, surveyActiveErr: errBoom}
	if _, err := newStubService(activeErr).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to load existing survey") {
		t.Fatalf("active err: %v", err)
	}

	active := &stubRepo{ticket: &models.Ticket{ID: 1}, surveyActive: &models.SatisfactionSurvey{ID: 7, SurveyToken: "tok"}}
	survey, err := newStubService(active).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1})
	if err != nil || survey == nil || survey.ID != 7 {
		t.Fatalf("expected active survey reuse, got %v %v", survey, err)
	}

	createErr := &stubRepo{ticket: &models.Ticket{ID: 1}, createSurveyErr: errBoom}
	if _, err := newStubService(createErr).ScheduleSurvey(context.Background(), &models.Ticket{ID: 1}); err == nil || !strings.Contains(err.Error(), "failed to schedule satisfaction survey") {
		t.Fatalf("create err: %v", err)
	}

	// chat 渠道（无邮件投递器语义）→ 直接 sent
	sent := &stubRepo{ticket: &models.Ticket{ID: 2, Source: "CHAT"}}
	survey, err = newStubService(sent).ScheduleSurvey(context.Background(), &models.Ticket{ID: 2, Source: "CHAT"})
	if err != nil {
		t.Fatal(err)
	}
	if survey.Status != "sent" || survey.Channel != "chat" || survey.SurveyToken == "" {
		t.Fatalf("chat survey = %+v", survey)
	}

	// email 渠道 + 已注入投递器 → queued
	queued := &stubRepo{ticket: &models.Ticket{ID: 3, Source: "EMAIL"}}
	survey, err = newStubService(queued).ScheduleSurvey(context.Background(), &models.Ticket{ID: 3, Source: "EMAIL"})
	if err != nil {
		t.Fatal(err)
	}
	if survey.Status != "queued" {
		t.Fatalf("email+mailer survey status = %q", survey.Status)
	}
}

func TestListSurveysStub(t *testing.T) {
	repo := &stubRepo{surveys: []models.SatisfactionSurvey{{ID: 1}}, surveysTotal: 1}
	req := &SatisfactionSurveyListRequest{Page: 0, PageSize: 500}
	surveys, total, err := newStubService(repo).ListSurveys(context.Background(), req)
	if err != nil || len(surveys) != 1 || total != 1 {
		t.Fatalf("ok: %v %v %v", surveys, total, err)
	}
	if req.Page != 1 || req.PageSize != 20 {
		t.Fatalf("page clamps = %d %d", req.Page, req.PageSize)
	}

	errRepo := &stubRepo{surveysErr: errBoom}
	if _, _, err := newStubService(errRepo).ListSurveys(context.Background(), &SatisfactionSurveyListRequest{}); err == nil || !strings.Contains(err.Error(), "failed to list satisfaction surveys") {
		t.Fatalf("err: %v", err)
	}
}

func TestGetSurveyPreviewByTokenStub(t *testing.T) {
	svc := newStubService(&stubRepo{})
	if _, err := svc.GetSurveyPreviewByToken(context.Background(), ""); err != ErrSurveyNotFound {
		t.Fatalf("empty token: %v", err)
	}

	notFound := &stubRepo{surveyByTokenErr: ErrSurveyNotFound}
	if _, err := newStubService(notFound).GetSurveyPreviewByToken(context.Background(), "x"); err != ErrSurveyNotFound {
		t.Fatalf("not found: %v", err)
	}

	tokenErr := &stubRepo{surveyByTokenErr: errBoom}
	if _, err := newStubService(tokenErr).GetSurveyPreviewByToken(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "failed to load survey") {
		t.Fatalf("token err: %v", err)
	}

	ticketErr := &stubRepo{surveyByToken: &models.SatisfactionSurvey{TicketID: 9}, ticketForAgentErr: errBoom}
	if _, err := newStubService(ticketErr).GetSurveyPreviewByToken(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "failed to load ticket for survey") {
		t.Fatalf("ticket err: %v", err)
	}

	agentless := &stubRepo{
		surveyByToken:  &models.SatisfactionSurvey{TicketID: 9},
		ticketForAgent: &models.Ticket{ID: 9, Title: "T"},
	}
	preview, err := newStubService(agentless).GetSurveyPreviewByToken(context.Background(), "x")
	if err != nil || preview.TicketTitle != "T" || preview.AgentName != "" {
		t.Fatalf("agentless: %+v %v", preview, err)
	}

	withAgent := &stubRepo{
		surveyByToken:  &models.SatisfactionSurvey{TicketID: 9},
		ticketForAgent: &models.Ticket{ID: 9, Agent: &models.User{Name: "Zoe"}},
	}
	preview, err = newStubService(withAgent).GetSurveyPreviewByToken(context.Background(), "x")
	if err != nil || preview.AgentName != "Zoe" {
		t.Fatalf("with agent: %+v %v", preview, err)
	}
}

func TestRespondSurveyStub(t *testing.T) {
	svc := newStubService(&stubRepo{})
	if _, err := svc.RespondSurvey(context.Background(), "", 5, ""); err != ErrSurveyNotFound {
		t.Fatalf("empty token: %v", err)
	}

	notFound := &stubRepo{surveyByTokenErr: ErrSurveyNotFound}
	if _, err := newStubService(notFound).RespondSurvey(context.Background(), "x", 5, ""); err != ErrSurveyNotFound {
		t.Fatalf("not found: %v", err)
	}

	loadErr := &stubRepo{surveyByTokenErr: errBoom}
	if _, err := newStubService(loadErr).RespondSurvey(context.Background(), "x", 5, ""); err == nil || !strings.Contains(err.Error(), "failed to load survey") {
		t.Fatalf("load err: %v", err)
	}

	completed := &stubRepo{surveyByToken: &models.SatisfactionSurvey{Status: "completed"}}
	if _, err := newStubService(completed).RespondSurvey(context.Background(), "x", 5, ""); err != ErrSurveyCompleted {
		t.Fatalf("completed: %v", err)
	}

	expired := &stubRepo{
		surveyByToken: &models.SatisfactionSurvey{
			ID:         4,
			ExpiresAt:  stubTimePtr(time.Now().Add(-time.Hour)),
			TicketID:   9,
			CustomerID: 9,
		},
	}
	if _, err := newStubService(expired).RespondSurvey(context.Background(), "x", 5, ""); err != ErrSurveyExpired {
		t.Fatalf("expired: %v", err)
	}
	if expired.expireSurveyCalls != 1 {
		t.Fatalf("expire must be called once, got %d", expired.expireSurveyCalls)
	}

	dupNoExisting := &stubRepo{
		surveyByToken:         &models.SatisfactionSurvey{TicketID: 9, CustomerID: 9},
		ticket:                &models.Ticket{ID: 9, CustomerID: 9},
		createSatisfactionErr: errors.New("satisfaction rating already exists for this ticket"),
	}
	if _, err := newStubService(dupNoExisting).RespondSurvey(context.Background(), "x", 5, ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dup without existing: %v", err)
	}

	dupWithExisting := &stubRepo{
		surveyByToken:         &models.SatisfactionSurvey{TicketID: 9, CustomerID: 9},
		ticket:                &models.Ticket{ID: 9, CustomerID: 9},
		createSatisfactionErr: errors.New("satisfaction rating already exists for this ticket"),
		satisfactionByTicket:  &models.CustomerSatisfaction{ID: 5},
	}
	if got, err := newStubService(dupWithExisting).RespondSurvey(context.Background(), "x", 5, ""); err != nil || got == nil || got.ID != 5 {
		t.Fatalf("dup with existing: %+v %v", got, err)
	}

	createErr := &stubRepo{
		surveyByToken:         &models.SatisfactionSurvey{TicketID: 9, CustomerID: 9},
		ticket:                &models.Ticket{ID: 9, CustomerID: 9},
		createSatisfactionErr: errBoom,
	}
	if _, err := newStubService(createErr).RespondSurvey(context.Background(), "x", 5, ""); err == nil || !strings.Contains(err.Error(), "failed to create satisfaction") {
		t.Fatalf("create err: %v", err)
	}

	completeWarn := &stubRepo{
		surveyByToken:     &models.SatisfactionSurvey{TicketID: 9, CustomerID: 9},
		ticket:            &models.Ticket{ID: 9, CustomerID: 9},
		satisfaction:      &models.CustomerSatisfaction{ID: 6},
		completeSurveyErr: errBoom,
	}
	if got, err := newStubService(completeWarn).RespondSurvey(context.Background(), "x", 5, ""); err != nil || got == nil || got.ID != 6 {
		t.Fatalf("complete warn: %+v %v", got, err)
	}
	if completeWarn.completeSurveyCalls != 1 {
		t.Fatalf("complete must be called once, got %d", completeWarn.completeSurveyCalls)
	}

	ok := &stubRepo{
		surveyByToken: &models.SatisfactionSurvey{TicketID: 9, CustomerID: 9},
		ticket:        &models.Ticket{ID: 9, CustomerID: 9},
		satisfaction:  &models.CustomerSatisfaction{ID: 7},
	}
	if got, err := newStubService(ok).RespondSurvey(context.Background(), "x", 5, ""); err != nil || got == nil || got.ID != 7 {
		t.Fatalf("ok: %+v %v", got, err)
	}
}

func TestResendSurveyStub(t *testing.T) {
	notFound := &stubRepo{surveyScopedErr: ErrSurveyNotFound}
	if _, err := newStubService(notFound).ResendSurvey(context.Background(), 1); err != ErrSurveyNotFound {
		t.Fatalf("not found: %v", err)
	}

	scopedErr := &stubRepo{surveyScopedErr: errBoom}
	if _, err := newStubService(scopedErr).ResendSurvey(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "failed to load survey") {
		t.Fatalf("scoped err: %v", err)
	}

	completed := &stubRepo{
		surveyScoped: &models.SatisfactionSurvey{ID: 1, Status: "completed", SatisfactionID: stubUintPtr(3)},
	}
	if _, err := newStubService(completed).ResendSurvey(context.Background(), 1); err != ErrSurveyCompleted {
		t.Fatalf("completed: %v", err)
	}

	saveErr := &stubRepo{surveyScoped: &models.SatisfactionSurvey{ID: 1, Status: "sent"}, saveSurveyErr: errBoom}
	if _, err := newStubService(saveErr).ResendSurvey(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "failed to resend survey") {
		t.Fatalf("save err: %v", err)
	}

	// 原 completed 保留 CompletedAt（不清完成字段）；无 token 生成新 token
	keep := &stubRepo{
		surveyScoped: &models.SatisfactionSurvey{
			ID:          1,
			Status:      "completed",
			Channel:     "email",
			SurveyToken: "",
			CompletedAt: stubTimePtr(time.Now()),
		},
	}
	survey, err := newStubService(keep).ResendSurvey(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if survey.Status != "queued" || survey.SurveyToken == "" || survey.CompletedAt == nil {
		t.Fatalf("keep completed: %+v", survey)
	}

	// 非 completed 原状态 → 清空完成字段
	drop := &stubRepo{surveyScoped: &models.SatisfactionSurvey{ID: 1, Status: "sent", Channel: "chat", SurveyToken: "old", CompletedAt: stubTimePtr(time.Now()), SatisfactionID: stubUintPtr(3)}}
	survey, err = newStubService(drop).ResendSurvey(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if survey.CompletedAt != nil || survey.SatisfactionID != nil {
		t.Fatalf("drop completed: %+v", survey)
	}
}

func TestProcessPendingSurveyEmailsStub(t *testing.T) {
	// 无投递器 → 直接 0（不入队投递）
	if n, err := NewService(&stubRepo{}, nil).ProcessPendingSurveyEmails(context.Background(), 10); err != nil || n != 0 {
		t.Fatalf("no mailer: %d %v", n, err)
	}

	expireErr := &stubRepo{expireOverdueErr: errBoom}
	if _, err := newStubService(expireErr).ProcessPendingSurveyEmails(context.Background(), 10); err == nil || !strings.Contains(err.Error(), "failed to expire overdue surveys") {
		t.Fatalf("expire err: %v", err)
	}

	listErr := &stubRepo{pendingErr: errBoom}
	if _, err := newStubService(listErr).ProcessPendingSurveyEmails(context.Background(), 10); err == nil || !strings.Contains(err.Error(), "failed to list pending surveys") {
		t.Fatalf("list err: %v", err)
	}

	noRecipient := &stubRepo{pendingSurveys: []models.SatisfactionSurvey{{ID: 1, CustomerID: 9}}, userErr: errBoom}
	if n, err := newStubService(noRecipient).ProcessPendingSurveyEmails(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("no recipient: %d %v", n, err)
	}

	failIgnored := &stubRepo{
		pendingSurveys: []models.SatisfactionSurvey{{ID: 1, CustomerID: 9}},
		userErr:        errBoom,
		markFailedErr:  errBoom, // best-effort 错误被吞
	}
	if _, err := newStubService(failIgnored).ProcessPendingSurveyEmails(context.Background(), 10); err != nil {
		t.Fatalf("mark failed err must be ignored: %v", err)
	}

	mailerFail := &stubRepo{
		pendingSurveys: []models.SatisfactionSurvey{{ID: 1, CustomerID: 9}},
		user:           &models.User{Email: "c@x.com"},
		userEmailOK:    true,
		markSentRows:   1,
	}
	svc := NewService(mailerFail, nil)
	mailer := &stubMailer{err: errBoom}
	svc.SetSurveyMailer(mailer)
	if n, err := svc.ProcessPendingSurveyEmails(context.Background(), 10); err != nil || n != 0 {
		t.Fatalf("mailer fail keeps queued: %d %v", n, err)
	}
	if mailer.calls != 1 {
		t.Fatalf("mailer calls = %d", mailer.calls)
	}

	markSentErr := &stubRepo{
		pendingSurveys: []models.SatisfactionSurvey{{ID: 1, CustomerID: 9}},
		user:           &models.User{Email: "c@x.com"},
		userEmailOK:    true,
		markSentErr:    errBoom,
	}
	if n, err := newStubService(markSentErr).ProcessPendingSurveyEmails(context.Background(), 10); err != nil || n != 0 {
		t.Fatalf("mark sent err: %d %v", n, err)
	}

	sent := &stubRepo{
		pendingSurveys: []models.SatisfactionSurvey{{ID: 1, CustomerID: 9}},
		user:           &models.User{Email: "c@x.com"},
		userEmailOK:    true,
		markSentRows:   1,
	}
	if n, err := newStubService(sent).ProcessPendingSurveyEmails(context.Background(), 10); err != nil || n != 1 {
		t.Fatalf("sent: %d %v", n, err)
	}
}

func TestCreateSatisfactionStub(t *testing.T) {
	scopedCtx := context.Background()
	ticket := &models.Ticket{ID: 5, CustomerID: 9, AgentID: stubUintPtr(7), TenantID: "t", WorkspaceID: "w"}

	ticketNotFound := &stubRepo{ticketErr: ErrTicketNotFound}
	if _, err := newStubService(ticketNotFound).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err != ErrTicketNotFound {
		t.Fatalf("ticket not found: %v", err)
	}

	ticketErr := &stubRepo{ticketErr: errBoom}
	if _, err := newStubService(ticketErr).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err == nil || !strings.Contains(err.Error(), "failed to validate ticket") {
		t.Fatalf("ticket err: %v", err)
	}

	customerNotFound := &stubRepo{ticket: ticket, customerErr: ErrCustomerNotFound}
	if _, err := newStubService(customerNotFound).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err != ErrCustomerNotFound {
		t.Fatalf("customer not found: %v", err)
	}

	customerErr := &stubRepo{ticket: ticket, customerErr: errBoom}
	if _, err := newStubService(customerErr).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err == nil || !strings.Contains(err.Error(), "failed to validate customer") {
		t.Fatalf("customer err: %v", err)
	}

	ownerMismatch := &stubRepo{ticket: ticket, customer: &models.Customer{UserID: 9}}
	if _, err := newStubService(ownerMismatch).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 10}); err == nil || !strings.Contains(err.Error(), "not the owner") {
		t.Fatalf("owner mismatch: %v", err)
	}

	dup := &stubRepo{ticket: ticket, customer: &models.Customer{UserID: 9}, findExisting: &models.CustomerSatisfaction{ID: 3}}
	if _, err := newStubService(dup).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dup: %v", err)
	}

	agentNotFound := &stubRepo{ticket: ticket, customer: &models.Customer{UserID: 9}, agentErr: ErrAgentNotFound}
	if _, err := newStubService(agentNotFound).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9, AgentID: stubUintPtr(7)}); err != ErrAgentNotFound {
		t.Fatalf("agent not found: %v", err)
	}

	agentErr := &stubRepo{ticket: ticket, customer: &models.Customer{UserID: 9}, agentErr: errBoom}
	if _, err := newStubService(agentErr).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9, AgentID: stubUintPtr(7)}); err == nil || !strings.Contains(err.Error(), "failed to validate agent") {
		t.Fatalf("agent err: %v", err)
	}

	insertErr := &stubRepo{ticket: ticket, customer: &models.Customer{UserID: 9}, createSatisfactionErr: errBoom}
	if _, err := newStubService(insertErr).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err == nil || !strings.Contains(err.Error(), "failed to create satisfaction") {
		t.Fatalf("insert err: %v", err)
	}

	preloadMiss := &stubRepo{
		ticket:          ticket,
		customer:        &models.Customer{UserID: 9},
		satisfactionErr: errBoom, // 预加载失败仅告警
	}
	if got, err := newStubService(preloadMiss).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9}); err != nil || got == nil {
		t.Fatalf("preload miss: %+v %v", got, err)
	}

	ok := &stubRepo{
		ticket:   ticket,
		customer: &models.Customer{UserID: 9},
		// 预加载按 ID 回读已落库的行，默认类目随插入一起持久化
		satisfaction: &models.CustomerSatisfaction{ID: 8, Category: "overall"},
	}
	got, err := newStubService(ok).CreateSatisfaction(scopedCtx, &SatisfactionCreateRequest{TicketID: 5, CustomerID: 9, Rating: 5})
	if err != nil || got == nil || got.ID != 8 || got.Category != "overall" {
		t.Fatalf("ok: %+v %v", got, err)
	}
}

func TestGetSatisfactionStub(t *testing.T) {
	notFound := &stubRepo{satisfactionErr: ErrSatisfactionNotFound}
	if _, err := newStubService(notFound).GetSatisfaction(context.Background(), 1); err == nil || err.Error() != "satisfaction not found" {
		t.Fatalf("not found: %v", err)
	}

	getErr := &stubRepo{satisfactionErr: errBoom}
	if _, err := newStubService(getErr).GetSatisfaction(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "failed to get satisfaction") {
		t.Fatalf("get err: %v", err)
	}

	ok := &stubRepo{satisfaction: &models.CustomerSatisfaction{ID: 2}}
	if got, err := newStubService(ok).GetSatisfaction(context.Background(), 2); err != nil || got == nil || got.ID != 2 {
		t.Fatalf("ok: %+v %v", got, err)
	}
}

func TestListSatisfactionsStub(t *testing.T) {
	repo := &stubRepo{satisfactions: []models.CustomerSatisfaction{{ID: 1}}, satisfactionsTotal: 1}
	items, total, err := newStubService(repo).ListSatisfactions(context.Background(), &SatisfactionListRequest{})
	if err != nil || len(items) != 1 || total != 1 {
		t.Fatalf("ok: %v %v %v", items, total, err)
	}

	errRepo := &stubRepo{satisfactionsErr: errBoom}
	if _, _, err := newStubService(errRepo).ListSatisfactions(context.Background(), &SatisfactionListRequest{}); err == nil || !strings.Contains(err.Error(), "failed to list satisfactions") {
		t.Fatalf("err: %v", err)
	}
}

func TestGetSatisfactionByTicketStub(t *testing.T) {
	repo := &stubRepo{satisfactionByTicket: &models.CustomerSatisfaction{ID: 4}}
	if got, err := newStubService(repo).GetSatisfactionByTicket(context.Background(), 1); err != nil || got == nil || got.ID != 4 {
		t.Fatalf("ok: %+v %v", got, err)
	}

	if got, err := newStubService(&stubRepo{}).GetSatisfactionByTicket(context.Background(), 1); err != nil || got != nil {
		t.Fatalf("nil: %v %v", got, err)
	}

	errRepo := &stubRepo{satisfactionByTicketErr: errBoom}
	if _, err := newStubService(errRepo).GetSatisfactionByTicket(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "failed to get satisfaction by ticket") {
		t.Fatalf("err: %v", err)
	}
}

func TestGetSatisfactionStatsStub(t *testing.T) {
	full := &stubRepo{
		statsCount: 10,
		statsAvg:   4.2,
		statsDist:  map[int]int{5: 8, 3: 2},
		statsCat:   map[string]SatisfactionStat{"overall": {Count: 10, AverageRating: 4.2}},
		statsTrend: []SatisfactionTrend{{Date: "2026-10-01", Count: 3, AverageRating: 4}},
	}
	resp, err := newStubService(full).GetSatisfactionStats(context.Background(), nil, nil)
	if err != nil || resp.TotalRatings != 10 || resp.AverageRating != 4.2 || len(resp.RatingDistribution) != 2 || resp.TrendData[0].Date != "2026-10-01" {
		t.Fatalf("full: %+v %v", resp, err)
	}

	dateFrom, dateTo := stubTimePtr(time.Now().Add(-time.Hour)), stubTimePtr(time.Now())
	resp, err = newStubService(full).GetSatisfactionStats(context.Background(), dateFrom, dateTo)
	if err != nil || resp.TrendData[0].Count != 3 {
		t.Fatalf("date range: %+v %v", resp, err)
	}

	countErr := &stubRepo{statsCountErr: errBoom}
	if _, err := newStubService(countErr).GetSatisfactionStats(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "failed to count ratings") {
		t.Fatalf("count err: %v", err)
	}

	avgErr := &stubRepo{statsAvgErr: errBoom}
	if _, err := newStubService(avgErr).GetSatisfactionStats(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "failed to calculate average rating") {
		t.Fatalf("avg err: %v", err)
	}

	distErr := &stubRepo{statsDistErr: errBoom}
	if _, err := newStubService(distErr).GetSatisfactionStats(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "failed to get rating distribution") {
		t.Fatalf("dist err: %v", err)
	}

	catErr := &stubRepo{statsCatErr: errBoom}
	if _, err := newStubService(catErr).GetSatisfactionStats(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "failed to get category stats") {
		t.Fatalf("cat err: %v", err)
	}

	// 趋势查询失败仅告警，不阻断
	trendErr := &stubRepo{statsCount: 1, statsTrendErr: errBoom}
	resp, err = newStubService(trendErr).GetSatisfactionStats(context.Background(), nil, nil)
	if err != nil || resp.TrendData != nil {
		t.Fatalf("trend warn: %+v %v", resp, err)
	}
}

func TestDeleteSatisfactionStub(t *testing.T) {
	rowsZero := &stubRepo{deleteSatisfactionRows: 0}
	if err := newStubService(rowsZero).DeleteSatisfaction(context.Background(), 1); err == nil || err.Error() != "satisfaction not found" {
		t.Fatalf("rows 0: %v", err)
	}

	deleteErr := &stubRepo{deleteSatisfactionErr: errBoom}
	if err := newStubService(deleteErr).DeleteSatisfaction(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "failed to delete satisfaction") {
		t.Fatalf("err: %v", err)
	}

	ok := &stubRepo{deleteSatisfactionRows: 1}
	if err := newStubService(ok).DeleteSatisfaction(context.Background(), 1); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

func TestUpdateSatisfactionStub(t *testing.T) {
	notFound := &stubRepo{findSatisfactionErr: ErrSatisfactionNotFound}
	if _, err := newStubService(notFound).UpdateSatisfaction(context.Background(), 1, "new"); err == nil || err.Error() != "satisfaction not found" {
		t.Fatalf("not found: %v", err)
	}

	findErr := &stubRepo{findSatisfactionErr: errBoom}
	if _, err := newStubService(findErr).UpdateSatisfaction(context.Background(), 1, "new"); err == nil || !strings.Contains(err.Error(), "failed to find satisfaction") {
		t.Fatalf("find err: %v", err)
	}

	saveErr := &stubRepo{findSatisfaction: &models.CustomerSatisfaction{ID: 1}, saveSatisfactionErr: errBoom}
	if _, err := newStubService(saveErr).UpdateSatisfaction(context.Background(), 1, "new"); err == nil || !strings.Contains(err.Error(), "failed to update satisfaction") {
		t.Fatalf("save err: %v", err)
	}

	// 重载失败仅告警，返回已保存记录
	reloadMiss := &stubRepo{
		findSatisfaction: &models.CustomerSatisfaction{ID: 1},
		satisfactionErr:  errBoom,
	}
	if got, err := newStubService(reloadMiss).UpdateSatisfaction(context.Background(), 1, "new"); err != nil || got == nil {
		t.Fatalf("reload miss: %+v %v", got, err)
	}

	ok := &stubRepo{
		findSatisfaction: &models.CustomerSatisfaction{ID: 1},
		satisfaction:     &models.CustomerSatisfaction{ID: 1, Comment: "new"},
	}
	got, err := newStubService(ok).UpdateSatisfaction(context.Background(), 1, "new")
	if err != nil || got == nil || got.ID != 1 || got.Comment != "new" {
		t.Fatalf("ok: %+v %v", got, err)
	}
}

func TestBuildSurveyEmailContentStub(t *testing.T) {
	subject, body := buildSurveyEmailContent(&models.SatisfactionSurvey{TicketID: 42, SurveyToken: "tok"}, "https://support.example.com")
	if !strings.Contains(subject, "42") || !strings.Contains(body, "https://support.example.com/csat/tok") {
		t.Fatalf("base url: %q %q", subject, body)
	}

	_, body = buildSurveyEmailContent(&models.SatisfactionSurvey{TicketID: 42, SurveyToken: "tok"}, "")
	if !strings.Contains(body, "/csat/tok") {
		t.Fatalf("relative link: %q", body)
	}
}

func stubTimePtr(v time.Time) *time.Time { return &v }

func stubUintPtr(v uint) *uint { return &v }
