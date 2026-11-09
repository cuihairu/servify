package infra

// 单测共用夹具（无 build tag）：原先住在 integration 标签文件里，导致负载
// 同步回归套件在默认门禁里不参与编译、覆盖率门持续红。移到无标签文件后，
// 带标签与不带标签的测试共享同一套夹具。

import (
	"strings"
	"testing"

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

func raceSeedAgent(t *testing.T, db *gorm.DB, userID uint, load int) models.Agent {
	t.Helper()
	agent := models.Agent{
		UserID:        userID,
		Status:        "online",
		MaxConcurrent: 10,
		CurrentLoad:   load,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent %d: %v", userID, err)
	}
	return agent
}

func raceSeedTicket(t *testing.T, db *gorm.DB, agentID *uint) models.Ticket {
	t.Helper()
	ticket := models.Ticket{
		Title:      "race ticket",
		CustomerID: 1,
		AgentID:    agentID,
		Status:     "assigned",
		Priority:   "normal",
		Category:   "support",
		Source:     "web",
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return ticket
}

func raceLoad(t *testing.T, db *gorm.DB, userID uint) int {
	t.Helper()
	var agent models.Agent
	if err := db.Where("user_id = ?", userID).First(&agent).Error; err != nil {
		t.Fatalf("load agent %d: %v", userID, err)
	}
	return agent.CurrentLoad
}

func raceTicketAgent(t *testing.T, db *gorm.DB, ticketID uint) *uint {
	t.Helper()
	var ticket models.Ticket
	if err := db.Select("id", "agent_id").First(&ticket, ticketID).Error; err != nil {
		t.Fatalf("load ticket %d: %v", ticketID, err)
	}
	return ticket.AgentID
}

func raceUintPtr(v uint) *uint { return &v }
