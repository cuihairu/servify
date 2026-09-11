//go:build integration && sqlite_integration
// +build integration,sqlite_integration

package infra

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTicketInfraTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := uniqueMemDSN("file:ticket_infra_" + strings.ReplaceAll(t.Name(), "/", "_") + "")
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
		&models.Ticket{},
		&models.CustomField{},
		&models.TicketCustomFieldValue{},
		&models.TicketStatus{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGormRepositoryListTicketCustomFields(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	seed := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true},
		{ID: 2, Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: true},
		{ID: 3, Resource: "customer", Key: "segment", Name: "Segment", Type: "string", Active: true},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed custom fields: %v", err)
	}
	if err := db.Model(&models.CustomField{}).Where("id = ?", 2).Update("active", false).Error; err != nil {
		t.Fatalf("deactivate seeded custom field: %v", err)
	}

	all, err := repo.ListTicketCustomFields(context.Background(), false)
	if err != nil {
		t.Fatalf("list all custom fields: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 ticket fields, got %d", len(all))
	}
	if all[0].Key != "severity" || all[1].Key != "region" {
		t.Fatalf("unexpected field ordering: %+v", all)
	}

	activeOnly, err := repo.ListTicketCustomFields(context.Background(), true)
	if err != nil {
		t.Fatalf("list active custom fields: %v", err)
	}
	if len(activeOnly) != 1 || activeOnly[0].Key != "severity" {
		t.Fatalf("unexpected active custom fields: %+v", activeOnly)
	}
}

func TestGormRepositoryFindAutoAssignableAgent(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	agents := []models.Agent{
		{UserID: 10, Status: "offline", MaxConcurrent: 5, CurrentLoad: 0, AvgResponseTime: 1},
		{UserID: 11, Status: "online", MaxConcurrent: 5, CurrentLoad: 2, AvgResponseTime: 20},
		{UserID: 12, Status: "online", MaxConcurrent: 5, CurrentLoad: 1, AvgResponseTime: 30},
		{UserID: 13, Status: "online", MaxConcurrent: 1, CurrentLoad: 1, AvgResponseTime: 1},
	}
	if err := db.Create(&agents).Error; err != nil {
		t.Fatalf("seed agents: %v", err)
	}

	agent, err := repo.FindAutoAssignableAgent(context.Background())
	if err != nil {
		t.Fatalf("find auto assignable agent: %v", err)
	}
	if agent.UserID != 12 {
		t.Fatalf("expected least-loaded online agent 12, got %+v", agent)
	}
}

func TestGormRepositoryUpdateTicketModelWithStatusAndCustomFields(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	ticket := &models.Ticket{
		ID:         1,
		Title:      "Old title",
		CustomerID: 1,
		Status:     "open",
		Priority:   "normal",
		Category:   "billing",
	}
	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "severity", Name: "Severity", Type: "select", Active: true},
		{ID: 2, Resource: "ticket", Key: "region", Name: "Region", Type: "string", Active: true},
		{ID: 3, Resource: "ticket", Key: "impact", Name: "Impact", Type: "string", Active: true},
	}
	values := []models.TicketCustomFieldValue{
		{TicketID: 1, CustomFieldID: 1, Value: "low"},
		{TicketID: 1, CustomFieldID: 2, Value: "cn"},
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if err := db.Create(&fields).Error; err != nil {
		t.Fatalf("seed custom field defs: %v", err)
	}
	if err := db.Create(&values).Error; err != nil {
		t.Fatalf("seed custom field values: %v", err)
	}

	now := time.Now()
	statusChange := &models.TicketStatus{
		UserID:     99,
		FromStatus: "open",
		ToStatus:   "resolved",
		Reason:     "状态更新",
		CreatedAt:  now,
	}
	upserts := []models.TicketCustomFieldValue{
		{CustomFieldID: 1, Value: "high", UpdatedAt: now},
		{CustomFieldID: 3, Value: "customer-facing", CreatedAt: now, UpdatedAt: now},
	}

	err := repo.UpdateTicketModelWithStatusAndCustomFields(
		context.Background(),
		1,
		map[string]interface{}{
			"title":      "New title",
			"status":     "resolved",
			"updated_at": now,
		},
		statusChange,
		false,
		[]uint{2},
		upserts,
	)
	if err != nil {
		t.Fatalf("update ticket with custom fields: %v", err)
	}

	var updated models.Ticket
	if err := db.First(&updated, 1).Error; err != nil {
		t.Fatalf("load updated ticket: %v", err)
	}
	if updated.Title != "New title" || updated.Status != "resolved" {
		t.Fatalf("unexpected updated ticket: %+v", updated)
	}

	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", 1).Find(&history).Error; err != nil {
		t.Fatalf("load ticket status history: %v", err)
	}
	if len(history) != 1 || history[0].ToStatus != "resolved" || history[0].UserID != 99 {
		t.Fatalf("unexpected status history: %+v", history)
	}

	var stored []models.TicketCustomFieldValue
	if err := db.Where("ticket_id = ?", 1).Order("custom_field_id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load custom field values: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 custom field values after delete/upsert, got %+v", stored)
	}
	if stored[0].CustomFieldID != 1 || stored[0].Value != "high" {
		t.Fatalf("expected existing field 1 to be updated, got %+v", stored[0])
	}
	if stored[1].CustomFieldID != 3 || stored[1].Value != "customer-facing" {
		t.Fatalf("expected field 3 to be inserted, got %+v", stored[1])
	}
}

func TestGormRepositoryUpdateTicketModelReturnsNotFoundWhenMissing(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	err := repo.UpdateTicketModel(context.Background(), 999, map[string]interface{}{"title": "missing"})
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}
