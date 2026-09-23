package orchestration

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newVisitorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:orchvisitor_"+t.Name()+"")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&models.Session{}))
	return db
}

func TestPrepareCreateVisitorTicketNilRequest(t *testing.T) {
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := o.PrepareCreateVisitorTicket(context.Background(), nil)
	require.ErrorContains(t, err, "request required")
}

func TestPrepareCreateVisitorTicketRequiresTitle(t *testing.T) {
	// HTTP 层 binding 已挡空 title；service 层再设一道（免认证端点纵深防御）。
	o := NewTicketOrchestrator(nil, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := o.PrepareCreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "m-1",
	})
	require.ErrorContains(t, err, "title required")
}

func TestPrepareCreateVisitorTicketSessionLookupFailure(t *testing.T) {
	db := newVisitorTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close()) // 关闭连接模拟底层故障（非 NotFound）
	o := NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, err = o.PrepareCreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "m-abc",
		Title:     "t",
	})
	require.ErrorContains(t, err, "session lookup failed")
}

func TestPrepareCreateVisitorTicketRequiresExistingSession(t *testing.T) {
	db := newVisitorTestDB(t)
	o := NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, err := o.PrepareCreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "missing-session",
		Title:     "打不开页面",
	})
	require.ErrorContains(t, err, "session not found")
}

func TestPrepareCreateVisitorTicketInheritsSessionScopeAndDefaults(t *testing.T) {
	db := newVisitorTestDB(t)
	require.NoError(t, db.Create(&models.Session{
		ID:          "m-abc123",
		TenantID:    "tenant-1",
		WorkspaceID: "ws-1",
		Status:      "active",
		Platform:    "chat",
	}).Error)
	o := NewTicketOrchestrator(db, logrus.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	prepared, err := o.PrepareCreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID:   "m-abc123",
		Title:       "打不开页面",
		Description: "点击按钮无响应",
		AISummary:   "访客反馈页面按钮无响应，已建议清除缓存",
	})
	require.NoError(t, err)

	ticket := prepared.Ticket
	require.NotNil(t, ticket.SessionID)
	require.Equal(t, "m-abc123", *ticket.SessionID)
	// scope 从 session 行原样继承（与 realtime 消息持久化同口径）。
	require.Equal(t, "tenant-1", ticket.TenantID)
	require.Equal(t, "ws-1", ticket.WorkspaceID)
	// 访客无 customer 主体；分类/优先级/来源为服务端固定默认值。
	require.Equal(t, uint(0), ticket.CustomerID)
	require.Equal(t, "打不开页面", ticket.Title)
	require.Equal(t, "点击按钮无响应", ticket.Description)
	require.Equal(t, "访客反馈页面按钮无响应，已建议清除缓存", ticket.AISummary)
	require.Equal(t, "general", ticket.Category)
	require.Equal(t, "normal", ticket.Priority)
	require.Equal(t, "chat", ticket.Source)
	require.Equal(t, "open", ticket.Status)
	// 无自定义字段面：访客请求不开放 custom_fields。
	require.Empty(t, prepared.CustomFieldValues)
}
