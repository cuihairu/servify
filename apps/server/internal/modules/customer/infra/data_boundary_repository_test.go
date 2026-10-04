package infra

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var boundaryDBSeq atomic.Uint64

func newBoundaryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:boundary_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(boundaryDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&models.User{}, &models.Customer{}, &models.Session{}, &models.Message{},
		&models.Ticket{}, &models.TicketComment{}, &models.TicketFile{},
	))
	return db
}

func boundaryScopeCtx() context.Context {
	return platformauth.ContextWithScope(context.Background(), "t9", "w9")
}

// seedBoundaryData 铺一条完整数据链：user → customer → session → message
// → ticket → comment → file。
func seedBoundaryData(t *testing.T, db *gorm.DB) (*models.User, *models.Customer) {
	t.Helper()
	user := &models.User{Username: "alice", Email: "alice@example.com", Name: "Alice PII", Phone: "13800000000", Role: "customer"}
	require.NoError(t, db.Create(user).Error)
	customer := &models.Customer{UserID: user.ID, Company: "Acme", Industry: "IT", Tags: "vip", Notes: "high-value notes", TenantID: "t9", WorkspaceID: "w9"}
	require.NoError(t, db.Create(customer).Error)
	session := &models.Session{ID: "sess-b1", UserID: user.ID, Status: "active", Platform: "web", StartedAt: time.Now(), TenantID: "t9", WorkspaceID: "w9"}
	require.NoError(t, db.Create(session).Error)
	require.NoError(t, db.Create(&models.Message{SessionID: session.ID, Content: "我的手机号是 13800000000", Sender: "customer", Type: "text"}).Error)
	ticket := &models.Ticket{Title: "退款工单", Description: "订单 123 退款", CustomerID: user.ID, Status: "open", Source: "web", TenantID: "t9", WorkspaceID: "w9"}
	require.NoError(t, db.Create(ticket).Error)
	require.NoError(t, db.Create(&models.TicketComment{TicketID: ticket.ID, UserID: user.ID, Content: "补充：越快越好", Type: "comment"}).Error)
	require.NoError(t, db.Create(&models.TicketFile{TicketID: ticket.ID, UserID: user.ID, FileName: "invoice.pdf", FilePath: "/uploads/invoice.pdf", FileSize: 100, MimeType: "application/pdf"}).Error)
	return user, customer
}

// B2-2 验收（PII 用例-导出）：导出覆盖清单中 Export=true 的全部类别，
// 凭证字段永不出现。
func TestDataBoundaryExportCoversCatalog(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	svc := customerapp.NewDataBoundaryService(repo)
	user, customer := seedBoundaryData(t, db)

	export, err := svc.ExportCustomerData(boundaryScopeCtx(), customer.ID)
	require.NoError(t, err)
	assert.Equal(t, customer.ID, export.Customer.ID)
	assert.Equal(t, user.ID, export.Profile.ID)
	require.Len(t, export.Sessions, 1)
	require.Len(t, export.Messages, 1)
	require.Contains(t, export.Messages[0].Content, "13800000000")
	require.Len(t, export.Tickets, 1)
	require.Len(t, export.Comments, 1)
	require.Len(t, export.Files, 1)
	// 清单齐全且含聊天记录/工单内容类别。
	require.NotEmpty(t, export.Catalog)
	categories := map[string]bool{}
	for _, item := range export.Catalog {
		categories[item.Category] = true
	}
	assert.True(t, categories["聊天记录"])
	assert.True(t, categories["工单内容"])
}

// B2-2 验收（PII 用例-删除）：擦除后身份/档案匿名化、消息与工单内容
// scrub、附件元数据删除；重复擦除幂等。
func TestDataBoundaryEraseScrubbsAllPII(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	svc := customerapp.NewDataBoundaryService(repo)
	user, customer := seedBoundaryData(t, db)

	result, err := svc.EraseCustomerData(boundaryScopeCtx(), customer.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SessionsHit)
	assert.Equal(t, int64(1), result.MessagesHit)
	assert.Equal(t, 1, result.TicketsHit)
	assert.Equal(t, int64(1), result.CommentsHit)
	assert.Equal(t, int64(1), result.FilesDeleted)

	var erasedUser models.User
	require.NoError(t, db.First(&erasedUser, user.ID).Error)
	assert.Equal(t, "erased-c"+strconv.FormatUint(uint64(customer.ID), 10), erasedUser.Username)
	assert.Contains(t, erasedUser.Email, "@erased.local")
	assert.NotContains(t, erasedUser.Name, "Alice")
	assert.Empty(t, erasedUser.Phone)

	var erasedCustomer models.Customer
	require.NoError(t, db.First(&erasedCustomer, customer.ID).Error)
	assert.Empty(t, erasedCustomer.Company)
	assert.Contains(t, erasedCustomer.Notes, customerapp.ErasureErasedText)

	var msg models.Message
	require.NoError(t, db.Where("session_id = ?", "sess-b1").First(&msg).Error)
	assert.Contains(t, msg.Content, customerapp.ErasureErasedText)
	assert.NotContains(t, msg.Content, "138")

	var ticket models.Ticket
	require.NoError(t, db.Where("customer_id = ?", user.ID).First(&ticket).Error)
	assert.Contains(t, ticket.Title, customerapp.ErasureErasedText)
	assert.NotContains(t, ticket.Description, "123")

	var fileCount int64
	require.NoError(t, db.Model(&models.TicketFile{}).Where("ticket_id = ?", ticket.ID).Count(&fileCount).Error)
	assert.Zero(t, fileCount)

	// 幂等：二次擦除不报错，结果仍一致。
	second, err := svc.EraseCustomerData(boundaryScopeCtx(), customer.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, second.SessionsHit)
}

// 数据主体不存在返回 ErrCustomerNotFound（读口 404 语义）。
func TestDataBoundaryNotFound(t *testing.T) {
	db := newBoundaryDB(t)
	repo := NewGormDataBoundaryRepository(db)
	svc := customerapp.NewDataBoundaryService(repo)
	for _, fn := range []func() error{
		func() error { _, err := svc.ExportCustomerData(boundaryScopeCtx(), 999); return err },
		func() error { _, err := svc.EraseCustomerData(boundaryScopeCtx(), 999); return err },
	} {
		require.ErrorIs(t, fn(), customerapp.ErrCustomerNotFound)
	}
}
