package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
)

// M3 移动 SDK 配套 §10 #4：访客工单创建端到端（免认证链 → session scope
// 继承 → 落库 → 坐席侧事件广播）。
func TestHandlerAssemblyCreateVisitorTicketFullFlow(t *testing.T) {
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)
	ctx := context.Background()

	if err := db.Create(&models.Session{
		ID:          "m-visitor1",
		TenantID:    "tenant-9",
		WorkspaceID: "ws-9",
		Status:      "active",
		Platform:    "chat",
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	got, err := adapter.CreateVisitorTicket(ctx, &ticketcontract.CreateVisitorTicketRequest{
		SessionID:   "m-visitor1",
		Title:       "打不开页面",
		Description: "点击按钮无响应",
		AISummary:   "访客反馈页面按钮无响应，已建议清除缓存",
	})
	if err != nil {
		t.Fatalf("CreateVisitorTicket() error = %v", err)
	}
	if got.ID == 0 || got.Status != "open" || got.Title != "打不开页面" {
		t.Fatalf("unexpected ticket: %+v", got)
	}
	// 访客无 customer 主体；scope 从 session 继承；ai_summary 落列。
	if got.CustomerID != 0 || got.TenantID != "tenant-9" || got.WorkspaceID != "ws-9" {
		t.Fatalf("unexpected scope/customer: %+v", got)
	}
	if got.AISummary != "访客反馈页面按钮无响应，已建议清除缓存" {
		t.Fatalf("unexpected ai_summary: %q", got.AISummary)
	}
	if got.SessionID == nil || *got.SessionID != "m-visitor1" {
		t.Fatalf("unexpected session binding: %+v", got.SessionID)
	}
	if got.Category != "general" || got.Priority != "normal" || got.Source != "chat" {
		t.Fatalf("unexpected server-fixed defaults: %+v", got)
	}

	// 落库行可被管理面按主键读回（坐席侧可见性的数据面）。
	stored, err := adapter.GetTicketByID(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetTicketByID() error = %v", err)
	}
	if stored.AISummary != got.AISummary || stored.TenantID != "tenant-9" {
		t.Fatalf("stored ticket mismatch: %+v", stored)
	}

	// 与管理面创建同构的副作用：状态史 + ticket.created 事件（坐席侧即时可见）。
	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", got.ID).Find(&history).Error; err != nil {
		t.Fatalf("load status history: %v", err)
	}
	if len(history) != 1 || history[0].ToStatus != "open" || history[0].Reason != "访客工单创建" {
		t.Fatalf("unexpected status history: %+v", history)
	}
	names := bus.published()
	if len(names) != 1 || names[0] != "ticket.created" {
		t.Fatalf("expected ticket.created event, got %v", names)
	}
}

func TestHandlerAssemblyCreateVisitorTicketSessionMissing(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	_, err := adapter.CreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "m-gone",
		Title:     "打不开页面",
	})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestHandlerAssemblyCreateVisitorTicketEmptyScopeFallsBackToDefaultMetric(t *testing.T) {
	// session 无租户 scope（单租户部署形态）：ticket scope 留空原样落库，
	// 指标口径兜底 default（与管理面 CreateTicket 的既有口径一致）。
	db := newTicketDeliveryDB(t)
	bus := &recordingBus{}
	adapter := newTicketDeliveryAdapter(t, db, bus)

	if err := db.Create(&models.Session{ID: "m-noscope", Status: "active", Platform: "chat"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	got, err := adapter.CreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "m-noscope",
		Title:     "咨询",
	})
	if err != nil {
		t.Fatalf("CreateVisitorTicket() error = %v", err)
	}
	if got.TenantID != "" || got.WorkspaceID != "" {
		t.Fatalf("expected empty scope passthrough, got %+v", got)
	}
}

func TestHandlerAssemblyCreateVisitorTicketPersistFailure(t *testing.T) {
	// tickets 表不存在 → session 解析成功但建行失败，错误透传。
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})

	if err := db.Create(&models.Session{ID: "m-x", Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Migrator().DropTable("tickets"); err != nil {
		t.Fatalf("drop tickets: %v", err)
	}

	_, err := adapter.CreateVisitorTicket(context.Background(), &ticketcontract.CreateVisitorTicketRequest{
		SessionID: "m-x",
		Title:     "打不开页面",
	})
	if err == nil {
		t.Fatal("expected persist error")
	}
}
