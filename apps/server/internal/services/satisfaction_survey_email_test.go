package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// fakeSurveyMailer 记录投递调用；failFor 按收件人注入失败。
type fakeSurveyMailer struct {
	sent    []fakeSentEmail
	failFor map[string]error
}

type fakeSentEmail struct {
	to, subject, body string
}

func (m *fakeSurveyMailer) SendSurveyEmail(_ context.Context, to, subject, body string) error {
	if err, ok := m.failFor[to]; ok {
		return err
	}
	m.sent = append(m.sent, fakeSentEmail{to: to, subject: subject, body: body})
	return nil
}

// TestScheduleSurvey_EmailWithMailer_QueuesWithoutSentAt 邮件渠道 + 已注入 mailer → queued 由 worker 投递
func TestScheduleSurvey_EmailWithMailer_QueuesWithoutSentAt(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)

	user := &models.User{ID: 1, Username: "customer1", Email: "c1@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	ticket := &models.Ticket{ID: 1, Title: "邮件工单", CustomerID: user.ID, Status: "closed", Source: "email", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	survey, err := svc.ScheduleSurvey(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ScheduleSurvey failed: %v", err)
	}
	if survey.Status != "queued" {
		t.Fatalf("expected queued, got %s", survey.Status)
	}
	if survey.SentAt != nil {
		t.Fatalf("queued survey must not have SentAt, got %v", survey.SentAt)
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("ScheduleSurvey must not send synchronously, got %d sends", len(mailer.sent))
	}
}

func TestScheduleSurvey_WithoutMailer_KeepsSentBehavior(t *testing.T) {
	svc, db := newSatisfactionTestService(t)

	user := &models.User{ID: 1, Username: "customer1", Email: "c1@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	ticket := &models.Ticket{ID: 1, Title: "邮件工单", CustomerID: user.ID, Status: "closed", Source: "email", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	survey, err := svc.ScheduleSurvey(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ScheduleSurvey failed: %v", err)
	}
	if survey.Status != "sent" {
		t.Fatalf("legacy behavior expected sent, got %s", survey.Status)
	}
}

func TestScheduleSurvey_NonEmailChannelWithMailer_KeepsSent(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	svc.SetSurveyMailer(&fakeSurveyMailer{})

	user := &models.User{ID: 1, Username: "customer1", Email: "c1@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	ticket := &models.Ticket{ID: 1, Title: "会话工单", CustomerID: user.ID, Status: "closed", Source: "chat", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	survey, err := svc.ScheduleSurvey(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ScheduleSurvey failed: %v", err)
	}
	if survey.Status != "sent" {
		t.Fatalf("non-email channel expected sent, got %s", survey.Status)
	}
}

func TestProcessPendingSurveyEmails_DeliversAndMarksSent(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)
	svc.SetSurveyLinkBaseURL("https://support.example.com/")

	now := time.Now()
	user := &models.User{ID: 7, Username: "customer7", Email: "c7@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	expires := now.Add(24 * time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 42, CustomerID: user.ID, Channel: "email", Status: "queued",
		SurveyToken: "tok-abc", ExpiresAt: &expires,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("create survey: %v", err)
	}

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 50)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails failed: %v", err)
	}
	if sent != 1 {
		t.Fatalf("expected 1 sent, got %d", sent)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(mailer.sent))
	}
	delivery := mailer.sent[0]
	if delivery.to != "c7@example.com" {
		t.Fatalf("unexpected recipient %s", delivery.to)
	}
	if !strings.Contains(delivery.body, "https://support.example.com/csat/tok-abc") {
		t.Fatalf("body missing survey link: %s", delivery.body)
	}

	var updated models.SatisfactionSurvey
	if err := db.First(&updated, survey.ID).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if updated.Status != "sent" || updated.SentAt == nil {
		t.Fatalf("expected sent with SentAt, got status=%s sent_at=%v", updated.Status, updated.SentAt)
	}
}

func TestProcessPendingSurveyEmails_SMTPFailure_KeepsQueued(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{failFor: map[string]error{
		"c7@example.com": context.DeadlineExceeded,
	}}
	svc.SetSurveyMailer(mailer)

	now := time.Now()
	user := &models.User{ID: 7, Username: "customer7", Email: "c7@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	expires := now.Add(24 * time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 42, CustomerID: user.ID, Channel: "email", Status: "queued",
		SurveyToken: "tok-abc", ExpiresAt: &expires,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("create survey: %v", err)
	}

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 50)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails failed: %v", err)
	}
	if sent != 0 {
		t.Fatalf("expected 0 sent on smtp failure, got %d", sent)
	}
	var updated models.SatisfactionSurvey
	if err := db.First(&updated, survey.ID).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if updated.Status != "queued" {
		t.Fatalf("transient failure must keep queued, got %s", updated.Status)
	}
}

func TestProcessPendingSurveyEmails_Expired_MarksFailed(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)

	now := time.Now()
	user := &models.User{ID: 7, Username: "customer7", Email: "c7@example.com"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	expires := now.Add(-time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 42, CustomerID: user.ID, Channel: "email", Status: "queued",
		SurveyToken: "tok-old", ExpiresAt: &expires,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("create survey: %v", err)
	}

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 50)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails failed: %v", err)
	}
	if sent != 0 || len(mailer.sent) != 0 {
		t.Fatalf("expired survey must not be delivered, sent=%d deliveries=%d", sent, len(mailer.sent))
	}
	var updated models.SatisfactionSurvey
	if err := db.First(&updated, survey.ID).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if updated.Status != "failed" {
		t.Fatalf("expired queued survey must be failed, got %s", updated.Status)
	}
}

func TestProcessPendingSurveyEmails_MissingRecipient_MarksFailed(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)

	now := time.Now()
	// 用户存在但没有邮箱
	user := &models.User{ID: 8, Username: "customer8", Email: ""}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	expires := now.Add(24 * time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 43, CustomerID: user.ID, Channel: "email", Status: "queued",
		SurveyToken: "tok-norecv", ExpiresAt: &expires,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("create survey: %v", err)
	}

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 50)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails failed: %v", err)
	}
	if sent != 0 || len(mailer.sent) != 0 {
		t.Fatalf("missing recipient must not be delivered, sent=%d deliveries=%d", sent, len(mailer.sent))
	}
	var updated models.SatisfactionSurvey
	if err := db.First(&updated, survey.ID).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if updated.Status != "failed" {
		t.Fatalf("missing recipient must be failed, got %s", updated.Status)
	}
}

func TestProcessPendingSurveyEmails_NilMailer_NoOp(t *testing.T) {
	svc, _ := newSatisfactionTestService(t)
	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent != 0 {
		t.Fatalf("nil mailer must be a no-op, got %d", sent)
	}
}
