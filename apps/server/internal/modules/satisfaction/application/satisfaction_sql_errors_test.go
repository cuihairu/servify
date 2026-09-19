package application

// satisfaction 落库错误分支：丢表、trigger 注入、顺序查询失败（自 services
// error_paths/more_branches/sequential_errors/trigger_error_paths 单测的
// satisfaction 段下沉）。

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// execTrigger 在测试库上建 SQLite trigger，注入写路径失败。
func execTrigger(t *testing.T, db *gorm.DB, stmt string) {
	t.Helper()
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

// failNthQuery 注册 gorm callback，强制第 n 次 SELECT 失败。
func failNthQuery(db *gorm.DB, n int32) {
	var calls int32
	bump := func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == n {
			_ = tx.AddError(errors.New("forced nth query failure"))
		}
	}
	_ = db.Callback().Query().Before("gorm:query").Register("fail_nth", bump)
	_ = db.Callback().Row().Before("gorm:row").Register("fail_nth_row", bump)
}

func TestSatisfactionService_DroppedTableErrors(t *testing.T) {
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, logrus.New())
	ctx := context.Background()

	if err := db.Migrator().DropTable("satisfaction_surveys"); err != nil {
		t.Fatalf("drop surveys: %v", err)
	}
	if _, err := svc.GetSurveyPreviewByToken(ctx, "tok"); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, err := svc.RespondSurvey(ctx, "tok", 5, ""); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, err := svc.ResendSurvey(ctx, 1); err == nil || err == ErrSurveyNotFound {
		t.Fatalf("expected load error, got %v", err)
	}
	if _, _, err := svc.ListSurveys(ctx, &SatisfactionSurveyListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list surveys error")
	}

	if err := db.Migrator().DropTable("customer_satisfactions"); err != nil {
		t.Fatalf("drop satisfactions: %v", err)
	}
	if _, err := svc.GetSatisfaction(ctx, 1); err == nil {
		t.Fatal("expected get satisfaction error")
	}
	if _, _, err := svc.ListSatisfactions(ctx, &SatisfactionListRequest{Page: 1, PageSize: 10}); err == nil {
		t.Fatal("expected list satisfactions error")
	}
	if _, err := svc.GetSatisfactionByTicket(ctx, 1); err == nil {
		t.Fatal("expected get by ticket error")
	}
	if _, err := svc.GetSatisfactionStats(ctx, nil, nil); err == nil {
		t.Fatal("expected stats error")
	}
	if err := svc.DeleteSatisfaction(ctx, 1); err == nil {
		t.Fatal("expected delete error")
	}
	if _, err := svc.UpdateSatisfaction(ctx, 1, "c"); err == nil {
		t.Fatal("expected update error")
	}
}

func TestSatisfactionService_ScheduleSurveyCountError(t *testing.T) {
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, logrus.New())
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Migrator().DropTable("customer_satisfactions"); err != nil {
		t.Fatalf("drop satisfactions: %v", err)
	}
	if _, err := svc.ScheduleSurvey(context.Background(), ticket); err == nil {
		t.Fatal("expected satisfaction count error")
	}
}

func TestSatisfactionService_TicketLoadErrors(t *testing.T) {
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, logrus.New())
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: 1, CustomerID: 1, Status: "sent", SurveyToken: "tok-err", SentAt: &past, ExpiresAt: &past,
		CreatedAt: past, UpdatedAt: past,
	}
	if err := db.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.GetSurveyPreviewByToken(ctx, "tok-err"); err == nil {
		t.Fatal("expected ticket preload error")
	}
	if _, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{TicketID: 1, CustomerID: 1, Rating: 5}); err == nil {
		t.Fatal("expected ticket lookup error")
	}
}

// ---- statistics error branches ----

// ---- router extras ----

func TestSatisfactionService_MoreErrorBranches(t *testing.T) {
	ctx := context.Background()

	// ScheduleSurvey: ticket validation query fails (non-notfound)
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, nil)
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}
	if _, err := svc.ScheduleSurvey(ctx, &models.Ticket{ID: 1}); err == nil || err.Error() == "ticket not found" {
		t.Fatalf("expected ticket query error, got %v", err)
	}

	// RespondSurvey: create fails without "already exists" marker
	db2 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc2 := NewService(db2, nil)
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer"}
	if err := db2.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db2.Create(&models.Customer{UserID: customer.ID}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db2.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	missingAgent := uint(999)
	survey := &models.SatisfactionSurvey{
		TicketID: ticket.ID, CustomerID: customer.ID, AgentID: &missingAgent,
		Status: "sent", SurveyToken: "tok-agent", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db2.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	if _, err := svc2.RespondSurvey(ctx, "tok-agent", 5, ""); err == nil {
		t.Fatal("expected agent-not-found error propagation")
	}

	// CreateSatisfaction: owner mismatch + agent query error
	db3 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc3 := NewService(db3, nil)
	owner := &models.User{Username: "owner", Email: "owner@x.com", Role: "customer"}
	if err := db3.Create(owner).Error; err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := db3.Create(&models.Customer{UserID: owner.ID}).Error; err != nil {
		t.Fatalf("seed owner profile: %v", err)
	}
	other := &models.User{Username: "other", Email: "other@x.com", Role: "customer"}
	if err := db3.Create(other).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}
	if err := db3.Create(&models.Customer{UserID: other.ID}).Error; err != nil {
		t.Fatalf("seed other profile: %v", err)
	}
	ticket3 := &models.Ticket{Title: "T3", CustomerID: owner.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db3.Create(ticket3).Error; err != nil {
		t.Fatalf("seed ticket3: %v", err)
	}
	if _, err := svc3.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket3.ID, CustomerID: other.ID, Rating: 5,
	}); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("expected owner mismatch error, got %v", err)
	}
	if err := db3.Migrator().DropTable("agents"); err != nil {
		t.Fatalf("drop agents: %v", err)
	}
	if _, err := svc3.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket3.ID, CustomerID: owner.ID, AgentID: &missingAgent, Rating: 5,
	}); err == nil {
		t.Fatal("expected agent query error")
	}
}

func TestSatisfactionService_SequentialErrors(t *testing.T) {
	ctx := context.Background()

	// ListSurveys find error
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	failNthQuery(db, 2)
	svc := NewService(db, nil)
	if _, _, err := svc.ListSurveys(ctx, &SatisfactionSurveyListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list satisfaction surveys") {
		t.Fatalf("surveys find error: %v", err)
	}

	// ListSatisfactions find error
	db2 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	failNthQuery(db2, 2)
	svc2 := NewService(db2, nil)
	if _, _, err := svc2.ListSatisfactions(ctx, &SatisfactionListRequest{Page: 1, PageSize: 10}); err == nil ||
		!strings.Contains(err.Error(), "failed to list satisfactions") {
		t.Fatalf("satisfactions find error: %v", err)
	}
}

func TestSatisfactionService_StatsSequentialErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		n          int32
		wantSubstr string
	}{
		{2, "failed to calculate average rating"},
		{3, "failed to get rating distribution"},
		{4, "failed to get category stats"},
	}
	for _, tc := range cases {
		db := newSatisfactionTestDB(t, &models.CustomerSatisfaction{})
		failNthQuery(db, tc.n)
		svc := NewService(db, nil)
		_, err := svc.GetSatisfactionStats(ctx, nil, nil)
		if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Fatalf("n=%d: expected %q, got %v", tc.n, tc.wantSubstr, err)
		}
	}

	// trend query failure only logs a warning
	db := newSatisfactionTestDB(t, &models.CustomerSatisfaction{})
	failNthQuery(db, 5)
	svc := NewService(db, nil)
	stats, err := svc.GetSatisfactionStats(ctx, nil, nil)
	if err != nil {
		t.Fatalf("trend failure should not propagate: %v", err)
	}
	if len(stats.TrendData) != 0 {
		t.Fatalf("expected empty trend, got %+v", stats.TrendData)
	}
}

func TestSatisfactionService_PreloadWarnings(t *testing.T) {
	ctx := context.Background()

	// CreateSatisfaction: reload (5th query) fails -> warning logged, no error
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, nil)
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.Customer{UserID: customer.ID}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ticket := &models.Ticket{Title: "T", CustomerID: customer.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	failNthQuery(db, 5)
	sat, err := svc.CreateSatisfaction(ctx, &SatisfactionCreateRequest{
		TicketID: ticket.ID, CustomerID: customer.ID, Rating: 5,
	})
	if err != nil {
		t.Fatalf("preload warning must not fail creation: %v", err)
	}
	if sat.Rating != 5 {
		t.Fatalf("unexpected satisfaction: %+v", sat)
	}

	// UpdateSatisfaction: reload (2nd query) fails -> warning logged, no error
	db2 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc2 := NewService(db2, nil)
	row := &models.CustomerSatisfaction{TicketID: 1, CustomerID: 1, Rating: 4, Comment: "old", CreatedAt: time.Now()}
	if err := db2.Create(row).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}
	failNthQuery(db2, 2)
	updated, err := svc2.UpdateSatisfaction(ctx, row.ID, "new")
	if err != nil {
		t.Fatalf("update preload warning must not fail: %v", err)
	}
	if updated.Comment != "new" {
		t.Fatalf("unexpected update: %+v", updated)
	}
}

func TestSatisfaction_InsertUpdateTriggerErrors(t *testing.T) {
	ctx := context.Background()

	// survey create fails after reuse-check
	db := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc := NewService(db, nil)
	ticket := &models.Ticket{Title: "T", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	execTrigger(t, db, "CREATE TRIGGER blk_survey BEFORE INSERT ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'insert blocked'); END;")
	if _, err := svc.ScheduleSurvey(ctx, ticket); err == nil {
		t.Fatal("expected survey insert error")
	}

	// RespondSurvey: satisfaction ok but survey status update fails
	db2 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc2 := NewService(db2, nil)
	customer := &models.User{Username: "c", Email: "c@x.com", Role: "customer"}
	if err := db2.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db2.Create(&models.Customer{UserID: customer.ID}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ticket2 := &models.Ticket{Title: "T2", CustomerID: customer.ID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db2.Create(ticket2).Error; err != nil {
		t.Fatalf("seed ticket2: %v", err)
	}
	future := time.Now().Add(time.Hour)
	survey := &models.SatisfactionSurvey{
		TicketID: ticket2.ID, CustomerID: customer.ID, Status: "sent", SurveyToken: "tok-trig",
		SentAt: &future, ExpiresAt: &future, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db2.Create(survey).Error; err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	execTrigger(t, db2, "CREATE TRIGGER blk_survey_upd BEFORE UPDATE ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	sat, err := svc2.RespondSurvey(ctx, "tok-trig", 4, "ok")
	if err != nil {
		t.Fatalf("RespondSurvey should tolerate status update failure: %v", err)
	}
	if sat == nil || sat.Rating != 4 {
		t.Fatalf("unexpected satisfaction: %+v", sat)
	}

	// ResendSurvey save failure
	db3 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc3 := NewService(db3, nil)
	survey3 := &models.SatisfactionSurvey{
		TicketID: 1, CustomerID: 1, Status: "sent", SurveyToken: "tok-trig3",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db3.Create(survey3).Error; err != nil {
		t.Fatalf("seed survey3: %v", err)
	}
	execTrigger(t, db3, "CREATE TRIGGER blk_survey3 BEFORE UPDATE ON satisfaction_surveys BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc3.ResendSurvey(ctx, survey3.ID); err == nil {
		t.Fatal("expected resend save error")
	}

	// UpdateSatisfaction save failure + preload miss
	db4 := newSatisfactionTestDB(t,
		&models.User{}, &models.Customer{}, &models.Agent{},
		&models.Ticket{}, &models.CustomerSatisfaction{}, &models.SatisfactionSurvey{},
	)
	svc4 := NewService(db4, nil)
	row := &models.CustomerSatisfaction{TicketID: 9, CustomerID: 9, Rating: 5, CreatedAt: time.Now()}
	if err := db4.Create(row).Error; err != nil {
		t.Fatalf("seed satisfaction: %v", err)
	}
	execTrigger(t, db4, "CREATE TRIGGER blk_sat_upd BEFORE UPDATE ON customer_satisfactions BEGIN SELECT RAISE(ABORT, 'update blocked'); END;")
	if _, err := svc4.UpdateSatisfaction(ctx, row.ID, "new"); err == nil {
		t.Fatal("expected update save error")
	}
}
