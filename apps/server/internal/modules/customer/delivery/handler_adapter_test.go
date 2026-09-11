package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	"servify/apps/server/internal/services"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newCustomerDeliveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := uniqueMemDSN("file:custdelivery_" + strings.ReplaceAll(t.Name(), "/", "_") + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}, &models.Customer{}, &models.Session{}, &models.Ticket{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func strPtr(v string) *string { return &v }

func TestHandlerServiceAdapterRoundTrip(t *testing.T) {
	db := newCustomerDeliveryTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	user, err := adapter.CreateCustomer(ctx, &services.CustomerCreateRequest{
		Username: "alice", Email: "alice@example.com", Name: "Alice", Phone: "123",
		Company: "A Co", Industry: "tech", Source: "web", Tags: " vip , beta ", Notes: "note1", Priority: "high",
	})
	require.NoError(t, err)
	assert.NotZero(t, user.ID)
	assert.Equal(t, "customer", user.Role)

	var stored models.Customer
	require.NoError(t, db.First(&stored, "user_id = ?", user.ID).Error)
	assert.Equal(t, "A Co", stored.Company)
	assert.Equal(t, "vip,beta", stored.Tags) // splitTags + application 层去重后落库

	fetched, err := adapter.GetCustomerByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice", fetched.Username)

	updated, err := adapter.UpdateCustomer(ctx, user.ID, &services.CustomerUpdateRequest{
		Name: strPtr("Alice2"), Phone: strPtr("456"), Company: strPtr("B Co"),
		Industry: strPtr("fin"), Source: strPtr("referral"), Notes: strPtr("note2"),
		Priority: strPtr("urgent"), Status: strPtr("inactive"), Tags: strPtr(" beta , gamma ,"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice2", updated.Name)
	assert.Equal(t, "inactive", updated.Status)
	require.NoError(t, db.First(&stored, "user_id = ?", user.ID).Error)
	assert.Equal(t, "B Co", stored.Company)
	assert.Equal(t, "beta,gamma", stored.Tags)

	// Tag 为 nil 时不更新 tags
	_, err = adapter.UpdateCustomer(ctx, user.ID, &services.CustomerUpdateRequest{Name: strPtr("Alice3")})
	require.NoError(t, err)

	items, total, err := adapter.ListCustomers(ctx, &services.CustomerListRequest{
		Page: 1, PageSize: 20, Industry: []string{"fin"}, Source: []string{"referral"},
		Priority: []string{"urgent"}, Status: []string{"inactive"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, user.ID, items[0].ID)
	assert.Equal(t, "B Co", items[0].Company)
	assert.Equal(t, "fin", items[0].Industry)
	assert.Equal(t, "referral", items[0].Source)
	assert.Equal(t, "beta,gamma", items[0].Tags)
	assert.Equal(t, "note2", items[0].Notes)
	assert.Equal(t, "urgent", items[0].Priority)

	now := time.Now()
	require.NoError(t, db.Create(&models.Session{
		ID: "sess-1", UserID: user.ID, Status: "active", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}).Error)
	agentID := user.ID + 100
	require.NoError(t, db.Create(&models.Ticket{
		Title: "tk-1", CustomerID: user.ID, AgentID: &agentID, Status: "open", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}).Error)
	require.NoError(t, db.Create(&models.Message{
		SessionID: "sess-1", UserID: user.ID, Type: "text", Content: "hello", CreatedAt: now.Add(-30 * time.Minute),
	}).Error)

	activity, err := adapter.GetCustomerActivity(ctx, user.ID, 10)
	require.NoError(t, err)
	assert.Equal(t, user.ID, activity.CustomerID)
	require.Len(t, activity.RecentSessions, 1)
	assert.Equal(t, "sess-1", activity.RecentSessions[0].ID)
	require.Len(t, activity.RecentTickets, 1)
	assert.Equal(t, "tk-1", activity.RecentTickets[0].Title)
	require.Len(t, activity.RecentMessages, 1)
	assert.Equal(t, "hello", activity.RecentMessages[0].Content)

	require.NoError(t, adapter.AddCustomerNote(ctx, user.ID, "hello note", 7))
	require.NoError(t, db.First(&stored, "user_id = ?", user.ID).Error)
	assert.Contains(t, stored.Notes, "用户7: hello note")

	require.NoError(t, adapter.UpdateCustomerTags(ctx, user.ID, []string{"x", "y"}))
	require.NoError(t, db.First(&stored, "user_id = ?", user.ID).Error)
	assert.Equal(t, "x,y", stored.Tags)

	require.NoError(t, db.Model(&models.User{}).Where("id = ?", user.ID).Update("last_login", time.Now()).Error)
	stats, err := adapter.GetCustomerStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Total)
	assert.Equal(t, int64(1), stats.Active)
	require.Len(t, stats.BySource, 1)
	assert.Equal(t, "referral", stats.BySource[0].Source)
	require.Len(t, stats.ByIndustry, 1)
	assert.Equal(t, "fin", stats.ByIndustry[0].Industry)
	require.Len(t, stats.ByPriority, 1)
	assert.Equal(t, "urgent", stats.ByPriority[0].Priority)

	version, err := adapter.RevokeCustomerTokens(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.Equal(t, 1, reloaded.TokenVersion)
}

type failingCustomerRepo struct{}

func (f *failingCustomerRepo) CreateCustomer(ctx context.Context, cmd customerapp.CreateCustomerCommand) (*models.User, error) {
	return nil, errors.New("boom-create")
}
func (f *failingCustomerRepo) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	return nil, errors.New("boom-get")
}
func (f *failingCustomerRepo) UpdateCustomer(ctx context.Context, customerID uint, cmd customerapp.UpdateCustomerCommand) (*models.User, error) {
	return nil, errors.New("boom-update")
}
func (f *failingCustomerRepo) ListCustomers(ctx context.Context, query customerapp.ListCustomersQuery) ([]customerapp.CustomerInfoDTO, int64, error) {
	return nil, 0, errors.New("boom-list")
}
func (f *failingCustomerRepo) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*customerapp.CustomerActivityDTO, error) {
	return nil, errors.New("boom-activity")
}
func (f *failingCustomerRepo) AddNote(ctx context.Context, customerID uint, note customerapp.CustomerNoteDTO) error {
	return errors.New("boom-note")
}
func (f *failingCustomerRepo) UpdateTags(ctx context.Context, customerID uint, tags []string) error {
	return errors.New("boom-tags")
}
func (f *failingCustomerRepo) GetStats(ctx context.Context) (*customerapp.CustomerStatsDTO, error) {
	return nil, errors.New("boom-stats")
}
func (f *failingCustomerRepo) RevokeCustomerTokens(ctx context.Context, customerID uint, revokeAt time.Time) (int, error) {
	return 0, errors.New("boom-revoke")
}

func TestHandlerServiceAdapterPropagatesErrors(t *testing.T) {
	adapter := NewHandlerServiceAdapter(customerapp.NewService(&failingCustomerRepo{}))
	ctx := context.Background()

	_, err := adapter.CreateCustomer(ctx, &services.CustomerCreateRequest{Username: "a", Email: "a@e.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-create")

	_, err = adapter.GetCustomerByID(ctx, 1)
	require.Error(t, err)

	_, err = adapter.UpdateCustomer(ctx, 1, &services.CustomerUpdateRequest{Name: strPtr("x")})
	require.Error(t, err)

	_, _, err = adapter.ListCustomers(ctx, &services.CustomerListRequest{Page: 1, PageSize: 10})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-list")

	_, err = adapter.GetCustomerActivity(ctx, 1, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-activity")

	err = adapter.AddCustomerNote(ctx, 1, "n", 2)
	require.Error(t, err)

	err = adapter.UpdateCustomerTags(ctx, 1, []string{"t"})
	require.Error(t, err)

	_, err = adapter.GetCustomerStats(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-stats")

	_, err = adapter.RevokeCustomerTokens(ctx, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-revoke")
}

func TestHandlerServiceAdapterNilDTOGuards(t *testing.T) {
	assert.Nil(t, customerActivityFromDTO(nil))
	assert.Nil(t, customerStatsFromDTO(nil))

	activity := customerActivityFromDTO(&customerapp.CustomerActivityDTO{
		CustomerID:     3,
		RecentSessions: []models.Session{{ID: "s1"}},
		RecentTickets:  []models.Ticket{{Title: "t1"}},
		RecentMessages: []models.Message{{Content: "m1"}},
	})
	require.NotNil(t, activity)
	assert.Equal(t, uint(3), activity.CustomerID)
	assert.Len(t, activity.RecentSessions, 1)

	stats := customerStatsFromDTO(&customerapp.CustomerStatsDTO{
		Total:       5,
		Active:      4,
		NewThisWeek: 2,
		BySource:    []customerapp.SourceCount{{Source: "web", Count: 3}},
		ByIndustry:  []customerapp.IndustryCount{{Industry: "tech", Count: 2}},
		ByPriority:  []customerapp.PriorityCount{{Priority: "high", Count: 1}},
	})
	require.NotNil(t, stats)
	assert.Equal(t, int64(5), stats.Total)
	assert.Equal(t, int64(4), stats.Active)
	assert.Equal(t, int64(2), stats.NewThisWeek)
	require.Len(t, stats.BySource, 1)
	assert.Equal(t, "web", stats.BySource[0].Source)
	require.Len(t, stats.ByIndustry, 1)
	assert.Equal(t, "tech", stats.ByIndustry[0].Industry)
	require.Len(t, stats.ByPriority, 1)
	assert.Equal(t, "high", stats.ByPriority[0].Priority)

	assert.Equal(t, services.CustomerInfo{
		User: models.User{ID: 9}, Company: "C", Industry: "I", Source: "S", Tags: "T", Notes: "N", Priority: "P",
	}, customerInfoFromDTO(customerapp.CustomerInfoDTO{
		User: models.User{ID: 9}, Company: "C", Industry: "I", Source: "S", Tags: "T", Notes: "N", Priority: "P",
	}))
}

func TestSplitTags(t *testing.T) {
	assert.Nil(t, splitTags(""))
	assert.Empty(t, splitTags(" , , "))
	assert.Equal(t, []string{"a", "b"}, splitTags(" a , ,b "))
	assert.Equal(t, []string{"a"}, splitTags("a"))
}
