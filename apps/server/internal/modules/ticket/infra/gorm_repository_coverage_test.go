package infra

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/application"
	"servify/apps/server/internal/modules/ticket/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var ticketCovDBSeq atomic.Uint64

func newTicketCovTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := "file:tkcov_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(ticketCovDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Session{},
		&models.Ticket{},
		&models.CustomField{},
		&models.TicketCustomFieldValue{},
		&models.TicketComment{},
		&models.TicketFile{},
		&models.TicketStatus{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func ticketCovScopeCtx(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func seedTicketCovUser(t *testing.T, db *gorm.DB, id uint, username string) models.User {
	t.Helper()
	user := models.User{
		ID: id, Username: username, Email: username + "@example.com",
		Role: "customer", Status: "active",
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func seedTicketCovTicket(t *testing.T, db *gorm.DB, id uint, title string, tenantID, workspaceID string) models.Ticket {
	t.Helper()
	ticket := models.Ticket{
		ID: id, Title: title, CustomerID: 1, Status: "open", Priority: "normal",
		Category: "technical", Source: "web", Tags: "vip,urgent",
		TenantID: tenantID, WorkspaceID: workspaceID,
	}
	require.NoError(t, db.Create(&ticket).Error)
	return ticket
}

func failTicketCovQueryWhen(db *gorm.DB, match func(sql string) bool) {
	handler := func(tx *gorm.DB) {
		if tx.Error == nil && match(tx.Statement.SQL.String()) {
			_ = tx.AddError(errors.New("forced query failure"))
		}
	}
	_ = db.Callback().Query().After("gorm:query").Replace("test:fail_query_when", handler)
	_ = db.Callback().Row().After("gorm:row").Replace("test:fail_query_when", handler)
	_ = db.Callback().Raw().After("gorm:raw").Replace("test:fail_query_when", handler)
}

func failTicketCovUpdateAt(db *gorm.DB, n int) {
	counter := 0
	_ = db.Callback().Update().Before("gorm:update").Replace("test:fail_update", func(tx *gorm.DB) {
		counter++
		if counter == n {
			_ = tx.AddError(errors.New("forced update failure"))
		}
	})
}

func failTicketCovCreateAt(db *gorm.DB, n int) {
	counter := 0
	_ = db.Callback().Create().Before("gorm:create").Replace("test:fail_create", func(tx *gorm.DB) {
		counter++
		if counter == n {
			_ = tx.AddError(errors.New("forced create failure"))
		}
	})
}

func TestTicketCovLoadAndGetTicket(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovUser(t, db, 1, "alice")
	agentID := uint(2)
	seedTicketCovUser(t, db, 2, "bob")
	require.NoError(t, db.Create(&models.Agent{UserID: 2, Status: "online", MaxConcurrent: 5}).Error)
	sessionID := "sess-1"
	require.NoError(t, db.Create(&models.Session{ID: sessionID, UserID: 1}).Error)
	ticket := seedTicketCovTicket(t, db, 10, "Need help", "t1", "w1")
	ticket.AgentID = &agentID
	ticket.SessionID = &sessionID
	require.NoError(t, db.Save(&ticket).Error)

	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 7, Value: "high"}).Error)
	require.NoError(t, db.Create(&models.TicketComment{TicketID: 10, UserID: 1, Content: "first", Type: "comment"}).Error)
	require.NoError(t, db.Create(&models.TicketStatus{TicketID: 10, UserID: 1, FromStatus: "", ToStatus: "open", Reason: "created"}).Error)
	require.NoError(t, db.Create(&models.TicketFile{TicketID: 10, UserID: 1, FileName: "a.txt", FilePath: "/tmp/a.txt"}).Error)

	model, err := repo.LoadTicketModelByID(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, "Need help", model.Title)
	assert.Equal(t, "alice", model.Customer.Username)
	assert.NotNil(t, model.Agent)
	assert.Equal(t, "bob", model.Agent.Username)
	assert.NotNil(t, model.Session)
	require.Len(t, model.Comments, 1)
	require.Len(t, model.StatusHistory, 1)
	require.Len(t, model.CustomFieldValues, 1)
	assert.Equal(t, "severity", model.CustomFieldValues[0].CustomField.Key)
	require.Len(t, model.Attachments, 1)

	details, err := repo.GetTicketByID(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, "Need help", details.Title)
	require.Len(t, details.CustomFieldValues, 1)
	assert.Equal(t, "severity", details.CustomFieldValues[0].Key)
	assert.Equal(t, "high", details.CustomFieldValues[0].Value)
	require.Len(t, details.Comments, 1)
	assert.Equal(t, "first", details.Comments[0].Content)
	require.Len(t, details.StatusHistory, 1)
	assert.Equal(t, "open", details.StatusHistory[0].ToStatus)

	_, err = repo.LoadTicketModelByID(ctx, 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticket not found")

	_, err = repo.GetTicketByID(ctx, 999)
	require.Error(t, err)

	simple, err := repo.GetTicket(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, uint(10), simple.ID)
	assert.Equal(t, "Need help", simple.Title)

	_, err = repo.GetTicket(ctx, 999)
	require.Error(t, err)
}

func TestTicketCovListTickets(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovUser(t, db, 1, "alice")
	agentID := uint(2)
	seedTicketCovUser(t, db, 2, "bob")
	require.NoError(t, db.Create(&models.Agent{UserID: 2, Status: "online"}).Error)
	now := time.Now()
	for i := 1; i <= 3; i++ {
		ticket := models.Ticket{
			ID: uint(i), Title: fmt.Sprintf("Ticket %d", i), Description: "searchable alpha",
			CustomerID: 1, AgentID: &agentID, Status: "open", Priority: "high",
			Category: "technical", Source: "web", Tags: "vip", TenantID: "t1", WorkspaceID: "w1",
			CreatedAt: now.Add(time.Duration(i) * time.Hour),
		}
		if i == 2 {
			ticket.Status = "resolved"
			ticket.Priority = "low"
		}
		require.NoError(t, db.Create(&ticket).Error)
	}
	require.NoError(t, db.Create(&models.Ticket{ID: 40, Title: "Other tenant", CustomerID: 1, Status: "open", TenantID: "t2"}).Error)
	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 1, CustomFieldID: 7, Value: "high"}).Error)

	query := application.ListTicketsQuery{
		Page: 1, PageSize: 10,
		Status:   []string{"open"},
		Priority: []string{"high"},
		Category: []string{"technical"},
		Source:   []string{"web"},
		Tag:      " vip ",
	}
	agentFilter := agentID
	customerFilter := uint(1)
	query.AgentID = &agentFilter
	query.CustomerID = &customerFilter
	query.Search = "  alpha  "

	items, total, err := repo.ListTickets(ctx, query)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	assert.Equal(t, "Ticket 3", items[0].Title)
	assert.Equal(t, agentID, *items[0].AgentID)

	filtered, total, err := repo.ListTicketModels(context.Background(), application.ListTicketsQuery{
		Page: 1, PageSize: 10,
		Status:             []string{"open"},
		Priority:           []string{"high"},
		Category:           []string{"technical"},
		Source:             []string{"web"},
		Tag:                " vip ",
		CustomFieldFilters: map[string]string{"severity": "high"},
		SortBy:             "title",
		SortOrder:          "asc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, filtered, 1)
	assert.Equal(t, "Ticket 1", filtered[0].Title)

	models1, total, err := repo.ListTicketModels(ctx, application.ListTicketsQuery{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, models1, 2)

	modelsDesc, _, err := repo.ListTicketModels(ctx, application.ListTicketsQuery{Page: 1, PageSize: 10, SortBy: "title", SortOrder: "DESC"})
	require.NoError(t, err)
	require.Len(t, modelsDesc, 3)
	assert.Equal(t, "Ticket 3", modelsDesc[0].Title)

	empty, total, err := repo.ListTicketModels(ctx, application.ListTicketsQuery{Page: 1, PageSize: 10, Tag: "  "})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, empty, 3)

	_, _, err = repo.ListTicketModels(ctx, application.ListTicketsQuery{Page: 1, PageSize: 10, SortBy: "bad_column"})
	require.Error(t, err)

	_, _, err = repo.ListTicketModels(ctx, application.ListTicketsQuery{Page: 1, PageSize: 10, Status: []string{"open"}, Priority: []string{"high"}, Category: []string{"technical"}, Source: []string{"web"}})
	require.NoError(t, err)
}

func TestTicketCovListTicketsCountError(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable("tickets"))

	_, _, err := repo.ListTicketModels(context.Background(), application.ListTicketsQuery{Page: 1, PageSize: 10})
	require.Error(t, err)

	_, _, err = repo.ListTickets(context.Background(), application.ListTicketsQuery{Page: 1, PageSize: 10})
	require.Error(t, err)
}

func TestTicketCovGetTicketStats(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovUser(t, db, 1, "alice")
	agentID := uint(2)
	// TodayCreated 按「今天零点」统计：种子取今天中午，避免午夜前后 -1h 掉进昨天导致 flaky
	now := time.Now()
	todayNoon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	require.NoError(t, db.Create(&models.Ticket{ID: 1, Title: "a", CustomerID: 1, AgentID: &agentID, Status: "open", Priority: "high", TenantID: "t1", WorkspaceID: "w1", CreatedAt: todayNoon}).Error)
	require.NoError(t, db.Create(&models.Ticket{ID: 2, Title: "b", CustomerID: 1, Status: "resolved", Priority: "low", TenantID: "t1", WorkspaceID: "w1", CreatedAt: todayNoon}).Error)
	require.NoError(t, db.Create(&models.Ticket{ID: 3, Title: "c", CustomerID: 1, Status: "resolved", Priority: "low", TenantID: "t2"}).Error)

	stats, err := repo.GetTicketStats(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Total)
	assert.Equal(t, int64(1), stats.Pending)
	assert.Equal(t, int64(1), stats.Resolved)
	assert.Equal(t, int64(2), stats.TodayCreated)
	require.Len(t, stats.ByStatus, 2)
	require.Len(t, stats.ByPriority, 2)

	agentStats, err := repo.GetTicketStats(ctx, &agentID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), agentStats.Total)
	assert.Equal(t, int64(1), agentStats.Pending)
}

func TestTicketCovGetTicketStatsQueryErrors(t *testing.T) {
	cases := []struct {
		name  string
		match func(sql string) bool
	}{
		{"total", func(sql string) bool {
			return strings.Contains(sql, "count(*)") && !strings.Contains(sql, "created_at >=")
		}},
		{"by_status", func(sql string) bool { return strings.Contains(sql, "GROUP BY") && strings.Contains(sql, "status") }},
		{"by_priority", func(sql string) bool { return strings.Contains(sql, "GROUP BY") && strings.Contains(sql, "priority") }},
		{"today", func(sql string) bool { return strings.Contains(sql, "created_at >=") }},
		{"pending", func(sql string) bool { return strings.Contains(sql, "status IN") }},
		{"resolved", func(sql string) bool { return strings.Contains(sql, "status =") && !strings.Contains(sql, "status IN") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTicketCovTestDB(t)
			repo := NewGormRepository(db)
			seedTicketCovUser(t, db, 1, "alice")
			require.NoError(t, db.Create(&models.Ticket{ID: 1, Title: "a", CustomerID: 1, Status: "open", TenantID: "t1"}).Error)
			failTicketCovQueryWhen(db, tc.match)

			_, err := repo.GetTicketStats(ticketCovScopeCtx("t1", "w1"), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "forced query failure")
		})
	}
}

func TestTicketCovListTicketCustomFields(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)

	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true, TenantID: "t1", WorkspaceID: "w1"},
		{ID: 2, Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: false, TenantID: "t1", WorkspaceID: "w1"},
		{ID: 3, Resource: "ticket", Key: "other", Name: "Other", Type: "string", Active: true, TenantID: "t2"},
		{ID: 4, Resource: "customer", Key: "segment", Name: "Segment", Type: "string", Active: true},
	}
	require.NoError(t, db.Create(&fields).Error)
	require.NoError(t, db.Model(&models.CustomField{}).Where("id = ?", 2).Update("active", false).Error)

	got, err := repo.ListTicketCustomFields(ticketCovScopeCtx("t1", "w1"), false)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "severity", got[0].Key)

	active, err := repo.ListTicketCustomFields(ticketCovScopeCtx("t1", "w1"), true)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "severity", active[0].Key)

	all, err := repo.ListTicketCustomFields(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	require.NoError(t, db.Migrator().DropTable("custom_fields"))
	_, err = repo.ListTicketCustomFields(context.Background(), false)
	require.Error(t, err)
}

func TestTicketCovCreateTicketAndModel(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	due := time.Now().Add(48 * time.Hour)
	agentID := uint(9)
	ticket := &domain.Ticket{
		Title: "New ticket", Description: "desc", CustomerID: 1, AgentID: &agentID,
		Category: "billing", Priority: "urgent", Status: "open", Source: "email",
		Tags: "new", DueDate: &due, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateTicket(ctx, ticket))
	assert.NotZero(t, ticket.ID)

	var stored models.Ticket
	require.NoError(t, db.First(&stored, ticket.ID).Error)
	assert.Equal(t, "t1", stored.TenantID)
	assert.Equal(t, "w1", stored.WorkspaceID)
	assert.Equal(t, "New ticket", stored.Title)

	model := &models.Ticket{Title: "With fields", CustomerID: 1, Status: "open"}
	require.NoError(t, repo.CreateTicketModelWithCustomFields(ctx, model, nil))
	assert.NotZero(t, model.ID)

	model2 := &models.Ticket{Title: "With status", CustomerID: 1, Status: "open"}
	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)
	err := repo.CreateTicketModelWithCustomFieldsAndStatus(ctx, model2,
		[]models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high"}},
		&models.TicketStatus{UserID: 1, FromStatus: "", ToStatus: "open", Reason: "created"})
	require.NoError(t, err)
	assert.NotZero(t, model2.ID)
	var count int64
	require.NoError(t, db.Model(&models.TicketCustomFieldValue{}).Where("ticket_id = ?", model2.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&models.TicketStatus{}).Where("ticket_id = ?", model2.ID).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// empty values, nil initial status
	model3 := &models.Ticket{Title: "Bare", CustomerID: 1, Status: "open"}
	require.NoError(t, repo.CreateTicketModelWithCustomFieldsAndStatus(ctx, model3, nil, nil))

	// create ticket fails: table dropped
	require.NoError(t, db.Migrator().DropTable("tickets"))
	err = repo.CreateTicket(ctx, &domain.Ticket{Title: "fail", CustomerID: 1})
	require.Error(t, err)
	err = repo.CreateTicketModelWithCustomFieldsAndStatus(ctx, &models.Ticket{Title: "fail"}, nil, nil)
	require.Error(t, err)
}

func TestTicketCovCreateTicketModelValuesError(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)

	dup := []models.TicketCustomFieldValue{
		{CustomFieldID: 7, Value: "a"},
		{CustomFieldID: 7, Value: "b"},
	}
	err := repo.CreateTicketModelWithCustomFieldsAndStatus(context.Background(), &models.Ticket{Title: "dup", CustomerID: 1}, dup, nil)
	require.Error(t, err)
}

func TestTicketCovCreateTicketModelStatusError(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable("ticket_statuses"))

	err := repo.CreateTicketModelWithCustomFieldsAndStatus(context.Background(), &models.Ticket{Title: "s", CustomerID: 1}, nil, &models.TicketStatus{ToStatus: "open"})
	require.Error(t, err)
}

func TestTicketCovUpdateTicket(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "Old", "t1", "w1")

	agentID := uint(3)
	ticket := &domain.Ticket{ID: 10, Title: "Updated", Description: "new desc", CustomerID: 1, AgentID: &agentID, Status: "in_progress", Priority: "high", Source: "chat", Tags: "t", UpdatedAt: time.Now()}
	require.NoError(t, repo.UpdateTicket(ctx, ticket))

	var stored models.Ticket
	require.NoError(t, db.First(&stored, 10).Error)
	assert.Equal(t, "Updated", stored.Title)
	assert.Equal(t, "in_progress", stored.Status)

	// not found (wrong tenant scope)
	wrongCtx := ticketCovScopeCtx("other", "")
	err := repo.UpdateTicket(wrongCtx, &domain.Ticket{ID: 10, Title: "Nope"})
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	err = repo.UpdateTicketModel(wrongCtx, 10, map[string]interface{}{"title": "Nope"})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	err = repo.UpdateTicketModel(ctx, 10, map[string]interface{}{"no_such_col": "x"})
	require.Error(t, err)
}

func TestTicketCovUpdateTicketWithStatus(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, repo.UpdateTicketWithStatus(ctx, &domain.Ticket{ID: 10, Title: "T", CustomerID: 1, Status: "resolved", UpdatedAt: time.Now()}, "open", 5, "done"))

	var history []models.TicketStatus
	require.NoError(t, db.Where("ticket_id = ?", 10).Find(&history).Error)
	require.Len(t, history, 1)
	assert.Equal(t, "resolved", history[0].ToStatus)

	// update failure
	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	require.NoError(t, db2.Migrator().DropTable("tickets"))
	err := repo2.UpdateTicketWithStatus(context.Background(), &domain.Ticket{ID: 1, Title: "x", Status: "closed"}, "open", 1, "r")
	require.Error(t, err)

	// status change failure
	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	require.NoError(t, db3.Migrator().DropTable("ticket_statuses"))
	err = repo3.UpdateTicketWithStatus(ctx, &domain.Ticket{ID: 10, Title: "T", CustomerID: 1, Status: "closed", UpdatedAt: time.Now()}, "open", 1, "r")
	require.Error(t, err)
}

func TestTicketCovUpdateTicketModelWithStatusAndCustomFieldsBranches(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 7, Value: "low"}).Error)

	// delete by ids + upsert update existing + insert new
	now := time.Now()
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 8, Value: "old"}).Error)
	err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, 10,
		map[string]interface{}{"title": "Branch"},
		&models.TicketStatus{UserID: 1, FromStatus: "open", ToStatus: "in_progress"},
		false,
		[]uint{8},
		[]models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high", UpdatedAt: now}, {CustomFieldID: 9, Value: "new", UpdatedAt: now}},
	)
	require.NoError(t, err)

	var stored models.Ticket
	require.NoError(t, db.First(&stored, 10).Error)
	assert.Equal(t, "Branch", stored.Title)
	var values []models.TicketCustomFieldValue
	require.NoError(t, db.Where("ticket_id = ?", 10).Order("custom_field_id ASC").Find(&values).Error)
	require.Len(t, values, 2)
	assert.Equal(t, "high", values[0].Value)
	assert.Equal(t, "new", values[1].Value)

	// clearAll
	err = repo.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, true, nil, nil)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&models.TicketCustomFieldValue{}).Where("ticket_id = ?", 10).Count(&count).Error)
	assert.Zero(t, count)

	// empty everything
	err = repo.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, map[string]interface{}{}, nil, false, nil, nil)
	require.NoError(t, err)
}

func TestTicketCovUpdateTicketModelWithStatusAndCustomFieldsErrors(t *testing.T) {
	ctx := ticketCovScopeCtx("t1", "w1")

	// invalid column in updates
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, map[string]interface{}{"no_such_col": "x"}, nil, false, nil, nil)
	require.Error(t, err)

	// status change insert fails
	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	seedTicketCovTicket(t, db2, 10, "T", "t1", "w1")
	require.NoError(t, db2.Migrator().DropTable("ticket_statuses"))
	err = repo2.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, &models.TicketStatus{ToStatus: "closed"}, false, nil, nil)
	require.Error(t, err)

	// clearAll delete fails
	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	require.NoError(t, db3.Migrator().DropTable("ticket_custom_field_values"))
	err = repo3.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, true, nil, nil)
	require.Error(t, err)

	// delete-by-ids fails
	db4 := newTicketCovTestDB(t)
	repo4 := NewGormRepository(db4)
	seedTicketCovTicket(t, db4, 10, "T", "t1", "w1")
	require.NoError(t, db4.Migrator().DropTable("ticket_custom_field_values"))
	err = repo4.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, false, []uint{1}, nil)
	require.Error(t, err)

	// upsert First fails with non-notfound error
	db5 := newTicketCovTestDB(t)
	repo5 := NewGormRepository(db5)
	seedTicketCovTicket(t, db5, 10, "T", "t1", "w1")
	require.NoError(t, db5.Migrator().DropTable("ticket_custom_field_values"))
	err = repo5.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "v"}})
	require.Error(t, err)

	// upsert Save fails
	db6 := newTicketCovTestDB(t)
	repo6 := NewGormRepository(db6)
	seedTicketCovTicket(t, db6, 10, "T", "t1", "w1")
	require.NoError(t, db6.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "s", Name: "S", Type: "string", Active: true}).Error)
	require.NoError(t, db6.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 7, Value: "low"}).Error)
	failTicketCovUpdateAt(db6, 1)
	err = repo6.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high", UpdatedAt: time.Now()}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forced update failure")

	// upsert Create fails
	db7 := newTicketCovTestDB(t)
	repo7 := NewGormRepository(db7)
	seedTicketCovTicket(t, db7, 10, "T", "t1", "w1")
	failTicketCovCreateAt(db7, 1)
	err = repo7.UpdateTicketModelWithStatusAndCustomFields(ctx, 10, nil, nil, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "v"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forced create failure")
}

func TestTicketCovSyncTicketCustomFieldValues(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, db.Create(&models.CustomField{ID: 7, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true}).Error)
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 7, Value: "low"}).Error)
	require.NoError(t, db.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 8, Value: "gone"}).Error)

	now := time.Now()
	require.NoError(t, repo.SyncTicketCustomFieldValues(ctx, 10, false, []uint{8}, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high", UpdatedAt: now}, {CustomFieldID: 9, Value: "new", UpdatedAt: now}}))

	var values []models.TicketCustomFieldValue
	require.NoError(t, db.Where("ticket_id = ?", 10).Order("custom_field_id ASC").Find(&values).Error)
	require.Len(t, values, 2)
	assert.Equal(t, "high", values[0].Value)
	assert.Equal(t, "new", values[1].Value)

	require.NoError(t, repo.SyncTicketCustomFieldValues(ctx, 10, true, nil, nil))
	var count int64
	require.NoError(t, db.Model(&models.TicketCustomFieldValue{}).Where("ticket_id = ?", 10).Count(&count).Error)
	assert.Zero(t, count)

	require.NoError(t, repo.SyncTicketCustomFieldValues(ctx, 10, false, nil, nil))
}

func TestTicketCovSyncTicketCustomFieldValuesErrors(t *testing.T) {
	ctx := ticketCovScopeCtx("t1", "w1")

	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, db.Migrator().DropTable("ticket_custom_field_values"))
	err := repo.SyncTicketCustomFieldValues(ctx, 10, true, nil, nil)
	require.Error(t, err)

	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	seedTicketCovTicket(t, db2, 10, "T", "t1", "w1")
	require.NoError(t, db2.Migrator().DropTable("ticket_custom_field_values"))
	err = repo2.SyncTicketCustomFieldValues(ctx, 10, false, []uint{1}, nil)
	require.Error(t, err)

	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	require.NoError(t, db3.Migrator().DropTable("ticket_custom_field_values"))
	err = repo3.SyncTicketCustomFieldValues(ctx, 10, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 1, Value: "v"}})
	require.Error(t, err)

	db4 := newTicketCovTestDB(t)
	repo4 := NewGormRepository(db4)
	seedTicketCovTicket(t, db4, 10, "T", "t1", "w1")
	require.NoError(t, db4.Create(&models.TicketCustomFieldValue{TicketID: 10, CustomFieldID: 7, Value: "low"}).Error)
	failTicketCovUpdateAt(db4, 1)
	err = repo4.SyncTicketCustomFieldValues(ctx, 10, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high", UpdatedAt: time.Now()}})
	require.Error(t, err)

	db5 := newTicketCovTestDB(t)
	repo5 := NewGormRepository(db5)
	seedTicketCovTicket(t, db5, 10, "T", "t1", "w1")
	failTicketCovCreateAt(db5, 1)
	err = repo5.SyncTicketCustomFieldValues(ctx, 10, false, nil, []models.TicketCustomFieldValue{{CustomFieldID: 7, Value: "high"}})
	require.Error(t, err)
}

func TestTicketCovAssignTicket(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	agentID := uint(4)
	require.NoError(t, db.Create(&models.Agent{UserID: 4, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}).Error)

	agentPtr := agentID
	previous := uint(0)
	require.NoError(t, repo.AssignTicket(ctx, &domain.Ticket{ID: 10, CustomerID: 1, Status: "assigned", AgentID: &agentPtr}, &previous, "open", 1, "assign"))

	var stored models.Ticket
	require.NoError(t, db.First(&stored, 10).Error)
	assert.Equal(t, agentID, *stored.AgentID)
	assert.Equal(t, "assigned", stored.Status)

	var agent models.Agent
	require.NoError(t, db.Where("user_id = ?", 4).First(&agent).Error)
	assert.Equal(t, 1, agent.CurrentLoad)

	// no agent on ticket (agent id resolves to 0), same status -> no status column update
	seedTicketCovTicket(t, db, 11, "T2", "t1", "w1")
	require.NoError(t, repo.AssignTicket(ctx, &domain.Ticket{ID: 11, CustomerID: 1, Status: "open"}, nil, "open", 1, "auto"))
	var stored2 models.Ticket
	require.NoError(t, db.First(&stored2, 11).Error)
	assert.Equal(t, "open", stored2.Status)

	// reassign decrements previous agent load
	require.NoError(t, db.Create(&models.Agent{UserID: 5, Status: "online", MaxConcurrent: 5, CurrentLoad: 3}).Error)
	newAgent := uint(5)
	require.NoError(t, repo.AssignTicket(ctx, &domain.Ticket{ID: 10, CustomerID: 1, Status: "assigned", AgentID: &newAgent}, &agentPtr, "assigned", 1, "reassign"))
	var oldAgent models.Agent
	require.NoError(t, db.Where("user_id = ?", 4).First(&oldAgent).Error)
	assert.Zero(t, oldAgent.CurrentLoad)

	// domain-level wrapper without agent
	seedTicketCovTicket(t, db, 12, "T3", "t1", "w1")
	require.NoError(t, repo.AssignTicket(ctx, &domain.Ticket{ID: 12, CustomerID: 1, Status: "assigned"}, nil, "open", 1, "auto"))
}

func TestTicketCovAssignTicketModelErrors(t *testing.T) {
	ctx := ticketCovScopeCtx("t1", "w1")

	// previous agent load update fails (agents table missing)
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, db.Migrator().DropTable("agents"))
	prev := uint(4)
	err := repo.AssignTicketModel(ctx, 10, 4, &prev, "open", "assigned", 1, "r")
	require.Error(t, err)

	// ticket updates fail (tickets table missing)
	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	require.NoError(t, db2.Migrator().DropTable("tickets"))
	err = repo2.AssignTicketModel(ctx, 10, 4, nil, "open", "assigned", 1, "r")
	require.Error(t, err)

	// ticket not found under scope
	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	err = repo3.AssignTicketModel(ticketCovScopeCtx("other", ""), 10, 4, nil, "open", "assigned", 1, "r")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// agent load increment fails
	db4 := newTicketCovTestDB(t)
	repo4 := NewGormRepository(db4)
	seedTicketCovTicket(t, db4, 10, "T", "t1", "w1")
	require.NoError(t, db4.Migrator().DropTable("agents"))
	err = repo4.AssignTicketModel(ctx, 10, 4, nil, "open", "assigned", 1, "r")
	require.Error(t, err)

	// status change insert fails
	db5 := newTicketCovTestDB(t)
	repo5 := NewGormRepository(db5)
	seedTicketCovTicket(t, db5, 10, "T", "t1", "w1")
	require.NoError(t, db5.Migrator().DropTable("ticket_statuses"))
	err = repo5.AssignTicketModel(ctx, 10, 4, nil, "open", "assigned", 1, "r")
	require.Error(t, err)
}

func TestTicketCovUnassignTicket(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	agentID := uint(4)
	ticket := seedTicketCovTicket(t, db, 11, "T2", "t1", "w1")
	ticket.AgentID = &agentID
	ticket.Status = "assigned"
	require.NoError(t, db.Save(&ticket).Error)
	require.NoError(t, db.Create(&models.Agent{UserID: 4, Status: "online", MaxConcurrent: 5, CurrentLoad: 2}).Error)

	agentPtr := agentID
	require.NoError(t, repo.UnassignTicket(ctx, &domain.Ticket{ID: 11, CustomerID: 1, Status: "open", AgentID: &agentPtr}, 4, "assigned", 1, "unassign"))

	var stored models.Ticket
	require.NoError(t, db.First(&stored, 11).Error)
	assert.Nil(t, stored.AgentID)
	assert.Equal(t, "open", stored.Status)

	var agent models.Agent
	require.NoError(t, db.Where("user_id = ?", 4).First(&agent).Error)
	assert.Equal(t, 1, agent.CurrentLoad)

	var history []models.TicketStatus
	require.NoError(t, db.Where("ticket_id = ?", 11).Find(&history).Error)
	require.Len(t, history, 1)

	// same status transition
	require.NoError(t, repo.UnassignTicketModel(ctx, 11, 4, "open", "open", 1, "same"))
	var storedAgain models.Ticket
	require.NoError(t, db.First(&storedAgain, 11).Error)
	assert.Equal(t, "open", storedAgain.Status)
}

func TestTicketCovUnassignTicketModelErrors(t *testing.T) {
	ctx := ticketCovScopeCtx("t1", "w1")

	// agent load update fails
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	require.NoError(t, db.Migrator().DropTable("agents"))
	err := repo.UnassignTicketModel(ctx, 10, 4, "assigned", "open", 1, "r")
	require.Error(t, err)

	// ticket updates fail
	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	require.NoError(t, db2.Migrator().DropTable("tickets"))
	err = repo2.UnassignTicketModel(ctx, 10, 4, "assigned", "open", 1, "r")
	require.Error(t, err)

	// ticket not found under scope
	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	err = repo3.UnassignTicketModel(ticketCovScopeCtx("other", ""), 10, 4, "assigned", "open", 1, "r")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// status change insert fails
	db4 := newTicketCovTestDB(t)
	repo4 := NewGormRepository(db4)
	seedTicketCovTicket(t, db4, 10, "T", "t1", "w1")
	require.NoError(t, db4.Migrator().DropTable("ticket_statuses"))
	err = repo4.UnassignTicketModel(ctx, 10, 4, "assigned", "open", 1, "r")
	require.Error(t, err)
}

func TestTicketCovCloseTicket(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	now := time.Now()
	require.NoError(t, repo.CloseTicket(ctx, &domain.Ticket{ID: 10, CustomerID: 1, Status: "closed", ClosedAt: &now, UpdatedAt: now}, "resolved", 1, "done"))

	var stored models.Ticket
	require.NoError(t, db.First(&stored, 10).Error)
	assert.Equal(t, "closed", stored.Status)
	assert.NotNil(t, stored.ClosedAt)

	// with agent: load decremented
	agentID := uint(4)
	ticket := seedTicketCovTicket(t, db, 11, "T2", "t1", "w1")
	ticket.AgentID = &agentID
	require.NoError(t, db.Save(&ticket).Error)
	require.NoError(t, db.Create(&models.Agent{UserID: 4, Status: "online", CurrentLoad: 2, MaxConcurrent: 5}).Error)
	agentPtr := agentID
	require.NoError(t, repo.CloseTicket(ctx, &domain.Ticket{ID: 11, CustomerID: 1, Status: "closed", AgentID: &agentPtr, ClosedAt: &now, UpdatedAt: now}, "resolved", 1, "done"))
	var agent models.Agent
	require.NoError(t, db.Where("user_id = ?", 4).First(&agent).Error)
	assert.Equal(t, 1, agent.CurrentLoad)
}

func TestTicketCovCloseTicketErrors(t *testing.T) {
	ctx := ticketCovScopeCtx("t1", "w1")

	// updates fail
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	require.NoError(t, db.Migrator().DropTable("tickets"))
	err := repo.CloseTicket(ctx, &domain.Ticket{ID: 10, Status: "closed", UpdatedAt: time.Now()}, "resolved", 1, "r")
	require.Error(t, err)

	// agent load update fails
	db2 := newTicketCovTestDB(t)
	repo2 := NewGormRepository(db2)
	seedTicketCovTicket(t, db2, 10, "T", "t1", "w1")
	require.NoError(t, db2.Migrator().DropTable("agents"))
	agentID := uint(4)
	err = repo2.CloseTicket(ctx, &domain.Ticket{ID: 10, CustomerID: 1, Status: "closed", AgentID: &agentID, UpdatedAt: time.Now()}, "resolved", 1, "r")
	require.Error(t, err)

	// status change insert fails
	db3 := newTicketCovTestDB(t)
	repo3 := NewGormRepository(db3)
	seedTicketCovTicket(t, db3, 10, "T", "t1", "w1")
	require.NoError(t, db3.Migrator().DropTable("ticket_statuses"))
	err = repo3.CloseTicket(ctx, &domain.Ticket{ID: 10, CustomerID: 1, Status: "closed", UpdatedAt: time.Now()}, "resolved", 1, "r")
	require.Error(t, err)
}

func TestTicketCovAddCommentAndRecordStatusChange(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)
	ctx := ticketCovScopeCtx("t1", "w1")

	seedTicketCovTicket(t, db, 10, "T", "t1", "w1")
	comment := &domain.Comment{UserID: 1, Content: "hello", Type: "comment", CreatedAt: time.Now()}
	require.NoError(t, repo.AddComment(ctx, 10, comment))
	assert.NotZero(t, comment.ID)

	change := &domain.StatusChange{UserID: 1, FromStatus: "open", ToStatus: "resolved", Reason: "done", CreatedAt: time.Now()}
	require.NoError(t, repo.RecordStatusChange(ctx, 10, change))
	assert.NotZero(t, change.ID)

	require.NoError(t, db.Migrator().DropTable("ticket_comments"))
	require.Error(t, repo.AddComment(ctx, 10, &domain.Comment{UserID: 1, Content: "x"}))

	require.NoError(t, db.Migrator().DropTable("ticket_statuses"))
	require.Error(t, repo.RecordStatusChange(ctx, 10, &domain.StatusChange{UserID: 1}))
}

func TestTicketCovExistenceChecks(t *testing.T) {
	db := newTicketCovTestDB(t)
	repo := NewGormRepository(db)

	emptyDB := newTicketCovTestDB(t)
	_, err := NewGormRepository(emptyDB).FindAutoAssignableAgent(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	seedTicketCovUser(t, db, 1, "alice")
	require.NoError(t, db.Create(&models.Agent{UserID: 2, Status: "online", MaxConcurrent: 5, CurrentLoad: 1}).Error)
	require.NoError(t, db.Create(&models.Agent{UserID: 3, Status: "offline", MaxConcurrent: 5, CurrentLoad: 0}).Error)

	ok, err := repo.CustomerExists(context.Background(), 1)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = repo.CustomerExists(context.Background(), 77)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = repo.AgentAssignable(context.Background(), 2)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = repo.AgentAssignable(context.Background(), 3)
	require.NoError(t, err)
	assert.False(t, ok)

	agent, err := repo.FindAutoAssignableAgent(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint(2), agent.UserID)

	require.NoError(t, db.Migrator().DropTable("users"))
	_, err = repo.CustomerExists(context.Background(), 1)
	require.Error(t, err)

	require.NoError(t, db.Migrator().DropTable("agents"))
	_, err = repo.AgentAssignable(context.Background(), 2)
	require.Error(t, err)
}

func TestTicketCovApplyTicketScopeFieldsNilModel(t *testing.T) {
	assert.NotPanics(t, func() {
		applyTicketScopeFields(ticketCovScopeCtx("t1", "w1"), nil)
	})
}
