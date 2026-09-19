package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

func seedEmailSurvey(t *testing.T, db *gorm.DB, id uint, customerID uint, status string) *models.SatisfactionSurvey {
	t.Helper()
	survey := &models.SatisfactionSurvey{
		ID:          id,
		TicketID:    id,
		CustomerID:  customerID,
		Channel:     "email",
		Status:      status,
		SurveyToken: "tok-" + time.Now().Format("150405.000000000"),
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	return survey
}

// TestResendSurveyEmailQueuesWithMailer 覆盖 email 渠道 + mailer 重发入队分支。
func TestResendSurveyEmailQueuesWithMailer(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	svc.SetSurveyMailer(&fakeSurveyMailer{})

	survey := seedEmailSurvey(t, db, 301, 1, "sent")

	resent, err := svc.ResendSurvey(context.Background(), survey.ID)
	if err != nil {
		t.Fatalf("ResendSurvey() error = %v", err)
	}
	if resent.Status != "queued" {
		t.Fatalf("status = %q, want queued", resent.Status)
	}
	if resent.SentAt != nil {
		t.Fatalf("queued resend must clear SentAt, got %v", resent.SentAt)
	}
	if resent.SurveyToken == "" {
		t.Fatal("expected survey token to be kept/generated")
	}

	var persisted models.SatisfactionSurvey
	if err := db.First(&persisted, survey.ID).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if persisted.Status != "queued" {
		t.Fatalf("persisted status = %q, want queued", persisted.Status)
	}
}

// TestProcessPendingSurveyEmailsDefaultBatch 覆盖 batchSize<=0 的默认值与成功投递。
func TestProcessPendingSurveyEmailsDefaultBatch(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)

	if err := db.Create(&models.User{ID: 311, Username: "c311", Email: "c311@x.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedEmailSurvey(t, db, 311, 311, "queued")

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 0)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails() error = %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].to != "c311@x.com" {
		t.Fatalf("mailer calls = %+v", mailer.sent)
	}
}

// TestProcessPendingSurveyEmailsMissingRecipient 覆盖收件人缺失置 failed。
func TestProcessPendingSurveyEmailsMissingRecipient(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	svc.SetSurveyMailer(&fakeSurveyMailer{})
	seedEmailSurvey(t, db, 321, 999999, "queued")

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}
	var survey models.SatisfactionSurvey
	if err := db.First(&survey, 321).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if survey.Status != "failed" {
		t.Fatalf("status = %q, want failed", survey.Status)
	}
}

// TestProcessPendingSurveyEmailsExpireError 覆盖过期兜底 UPDATE 失败透传。
func TestProcessPendingSurveyEmailsExpireError(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	svc.SetSurveyMailer(&fakeSurveyMailer{})
	if err := db.Migrator().DropTable("satisfaction_surveys"); err != nil {
		t.Fatalf("drop surveys: %v", err)
	}
	if _, err := svc.ProcessPendingSurveyEmails(context.Background(), 10); err == nil {
		t.Fatal("expected expire update error")
	}
}

// TestProcessPendingSurveyEmailsListError 覆盖 queued 扫描失败透传。
func TestProcessPendingSurveyEmailsListError(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	svc.SetSurveyMailer(&fakeSurveyMailer{})
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_survey_query", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "satisfaction_surveys" {
			_ = tx.AddError(errors.New("boom survey list"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	if _, err := svc.ProcessPendingSurveyEmails(context.Background(), 10); err == nil || !strings.Contains(err.Error(), "failed to list pending surveys") {
		t.Fatalf("err = %v, want list failure", err)
	}
}

// TestProcessPendingSurveyEmailsMarkSentError 覆盖投递成功后置 sent 失败（保 queued）。
func TestProcessPendingSurveyEmailsMarkSentError(t *testing.T) {
	svc, db := newSatisfactionTestService(t)
	mailer := &fakeSurveyMailer{}
	svc.SetSurveyMailer(mailer)

	if err := db.Create(&models.User{ID: 331, Username: "c331", Email: "c331@x.com"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedEmailSurvey(t, db, 331, 331, "queued")

	updates := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_second_survey_update", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "satisfaction_surveys" {
			updates++
			if updates >= 2 { // 第一次是过期兜底 UPDATE，第二次是置 sent
				_ = tx.AddError(errors.New("boom mark sent"))
			}
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	sent, err := svc.ProcessPendingSurveyEmails(context.Background(), 10)
	if err != nil {
		t.Fatalf("ProcessPendingSurveyEmails() error = %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent = %d, want 0", sent)
	}
	var survey models.SatisfactionSurvey
	if err := db.First(&survey, 331).Error; err != nil {
		t.Fatalf("reload survey: %v", err)
	}
	if survey.Status != "queued" {
		t.Fatalf("status = %q, want queued after mark-sent failure", survey.Status)
	}
}
