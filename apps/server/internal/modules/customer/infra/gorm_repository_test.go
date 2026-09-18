package infra

import (
	"context"
	"errors"
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

var customerRepoDBSeq atomic.Uint64

func newCustomerRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:custrepo_" + strings.ReplaceAll(t.Name(), "/", "_") + "_" + strconv.FormatUint(customerRepoDBSeq.Add(1), 10) + "?mode=memory&cache=shared"
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

func customerScopeCtx(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func seedCustomerRow(t *testing.T, db *gorm.DB, userID uint, username string, tenantID, workspaceID string, now time.Time) models.Customer {
	t.Helper()
	user := models.User{
		ID: userID, Username: username, Email: username + "@example.com",
		Role: "customer", Status: "active", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	customer := models.Customer{
		UserID: userID, TenantID: tenantID, WorkspaceID: workspaceID,
		Company: username + " Co", Source: "web", Priority: "normal",
		CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
	}
	if err := db.Create(&customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return customer
}

func TestCustomerRepoCreateAndGetByIDRoundTrip(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	ctx := customerScopeCtx("t1", "w1")

	created, err := repo.CreateCustomer(ctx, customerapp.CreateCustomerCommand{
		Username: "alice", Email: "alice@example.com", Name: "Alice", Phone: "123",
		Company: "A Co", Industry: "tech", Source: "referral",
		Tags: []string{"vip", "beta", "vip"}, Notes: "first note", Priority: "high",
	})
	require.NoError(t, err)
	assert.Equal(t, "customer", created.Role)
	assert.Equal(t, "active", created.Status)
	assert.Equal(t, "alice", created.Username)

	var stored models.Customer
	require.NoError(t, db.First(&stored, "user_id = ?", created.ID).Error)
	assert.Equal(t, "t1", stored.TenantID)
	assert.Equal(t, "w1", stored.WorkspaceID)
	assert.Equal(t, "A Co", stored.Company)
	assert.Equal(t, "tech", stored.Industry)
	assert.Equal(t, "referral", stored.Source)
	assert.Equal(t, "vip,beta,vip", stored.Tags) // infra 层不做去重，去重在 application 层
	assert.Equal(t, "first note", stored.Notes)
	assert.Equal(t, "high", stored.Priority)

	now := time.Now()
	require.NoError(t, db.Create(&[]models.Session{
		{ID: "s-yes", UserID: created.ID, Status: "active", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "s-no", UserID: created.ID, Status: "active", TenantID: "t1", WorkspaceID: "w2", CreatedAt: now, UpdatedAt: now},
	}).Error)
	agentID := created.ID + 100
	require.NoError(t, db.Create(&[]models.Ticket{
		{Title: "t-yes", CustomerID: created.ID, AgentID: &agentID, Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{Title: "t-no", CustomerID: created.ID, AgentID: &agentID, Status: "open", TenantID: "t1", WorkspaceID: "w2", CreatedAt: now, UpdatedAt: now},
	}).Error)

	fetched, err := repo.GetCustomerByID(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, fetched.Sessions, 1)
	assert.Equal(t, "s-yes", fetched.Sessions[0].ID)
	require.Len(t, fetched.Tickets, 1)
	assert.Equal(t, "t-yes", fetched.Tickets[0].Title)

	// 无租户上下文时不追加 scope 过滤，预加载返回全部
	unscoped, err := repo.GetCustomerByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Len(t, unscoped.Sessions, 2)
	assert.Len(t, unscoped.Tickets, 2)

	_, err = repo.GetCustomerByID(ctx, created.ID+999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")
}

func TestCustomerRepoCreateCustomerRejectsDuplicates(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	_, err := repo.CreateCustomer(ctx, customerapp.CreateCustomerCommand{Username: "alice", Email: "alice@example.com"})
	require.NoError(t, err)

	_, err = repo.CreateCustomer(ctx, customerapp.CreateCustomerCommand{Username: "alice2", Email: "alice@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username or email already exists")

	_, err = repo.CreateCustomer(ctx, customerapp.CreateCustomerCommand{Username: "alice", Email: "other@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username or email already exists")
}

func TestCustomerRepoCreateCustomerTableFailures(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.Customer{}))
	repo := NewGormRepository(db)
	_, err := repo.CreateCustomer(context.Background(), customerapp.CreateCustomerCommand{Username: "a", Email: "a@e.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create customer info")

	db2 := newCustomerRepoTestDB(t)
	require.NoError(t, db2.Migrator().DropTable(&models.User{}))
	repo2 := NewGormRepository(db2)
	_, err = repo2.CreateCustomer(context.Background(), customerapp.CreateCustomerCommand{Username: "a", Email: "a@e.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create user")
}

func TestCustomerRepoUpdateCustomerFields(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "", "", now)

	strPtr := func(v string) *string { return &v }
	tags := []string{"vip", "gold"}
	updated, err := repo.UpdateCustomer(context.Background(), 1, customerapp.UpdateCustomerCommand{
		Name: strPtr("Alice2"), Phone: strPtr("456"), Status: strPtr("inactive"),
		Company: strPtr("B Co"), Industry: strPtr("fin"), Source: strPtr("ads"),
		Tags: &tags, Notes: strPtr("note2"), Priority: strPtr("urgent"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice2", updated.Name)
	assert.Equal(t, "456", updated.Phone)
	assert.Equal(t, "inactive", updated.Status)

	var stored models.Customer
	require.NoError(t, db.First(&stored, "user_id = ?", 1).Error)
	assert.Equal(t, "B Co", stored.Company)
	assert.Equal(t, "fin", stored.Industry)
	assert.Equal(t, "ads", stored.Source)
	assert.Equal(t, "vip,gold", stored.Tags)
	assert.Equal(t, "note2", stored.Notes)
	assert.Equal(t, "urgent", stored.Priority)

	// 空更新命令：直接返回当前客户
	noop, err := repo.UpdateCustomer(context.Background(), 1, customerapp.UpdateCustomerCommand{})
	require.NoError(t, err)
	assert.Equal(t, uint(1), noop.ID)

	_, err = repo.UpdateCustomer(context.Background(), 999, customerapp.UpdateCustomerCommand{Name: strPtr("x")})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))

	_, err = repo.UpdateCustomer(context.Background(), 999, customerapp.UpdateCustomerCommand{Company: strPtr("x")})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestCustomerRepoUpdateCustomerTableFailures(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.User{}))
	repo := NewGormRepository(db)
	name := "x"
	_, err := repo.UpdateCustomer(context.Background(), 1, customerapp.UpdateCustomerCommand{Name: &name})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update user")

	db2 := newCustomerRepoTestDB(t)
	require.NoError(t, db2.Migrator().DropTable(&models.Customer{}))
	repo2 := NewGormRepository(db2)
	company := "x"
	_, err = repo2.UpdateCustomer(context.Background(), 1, customerapp.UpdateCustomerCommand{Company: &company})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update customer")
}

func seedCustomerListRows(t *testing.T, db *gorm.DB, now time.Time) {
	t.Helper()
	users := []models.User{
		{ID: 1, Username: "u1", Email: "u1@e.com", Role: "customer", Status: "active", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now},
		{ID: 2, Username: "u2", Email: "u2@e.com", Role: "customer", Status: "inactive", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now},
		{ID: 3, Username: "u3", Email: "u3@e.com", Role: "customer", Status: "active", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now},
		{ID: 4, Username: "agent", Email: "ag@e.com", Role: "agent", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&users).Error)
	customers := []models.Customer{
		{UserID: 1, TenantID: "t1", WorkspaceID: "w1", Company: "C1", Industry: "tech", Source: "web", Priority: "high", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now},
		{UserID: 2, TenantID: "t2", WorkspaceID: "w2", Company: "C2", Industry: "retail", Source: "referral", Priority: "normal", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now},
		{UserID: 3, TenantID: "t1", WorkspaceID: "w1", Company: "C3", Industry: "tech", Source: "web", Priority: "low", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now},
	}
	require.NoError(t, db.Create(&customers).Error)
}

func TestCustomerRepoListCustomersFiltersPagingAndScope(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerListRows(t, db, now)

	items, total, err := repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, items, 3)
	assert.Equal(t, uint(3), items[0].ID)
	assert.Equal(t, "C3", items[0].Company)
	assert.Equal(t, "tech", items[0].Industry)
	assert.Equal(t, "web", items[0].Source)
	assert.Equal(t, "low", items[0].Priority)

	items, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Industry: []string{"tech"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, items, 2)

	_, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Source: []string{"referral"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	_, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Priority: []string{"high"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	_, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Status: []string{"inactive"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	items, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Industry: []string{"tech"}, Source: []string{"web"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, items, 2)

	items, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 2, PageSize: 2, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, items, 1)
	assert.Equal(t, uint(1), items[0].ID)

	items, total, err = repo.ListCustomers(customerScopeCtx("t1", "w1"), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, items, 2)
	assert.Equal(t, uint(1), items[1].ID)
}

func TestCustomerRepoListCustomersTagAndSearchFilters(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	seedCustomerListRows(t, db, time.Now())
	require.NoError(t, db.Model(&models.Customer{}).Where("user_id = ?", 1).Update("tags", "vip,enterprise").Error)

	// LOWER(col) LIKE LOWER(?) 跨方言筛选：sqlite 与 pg 行为一致。
	tagged, total, err := repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Tags: []string{"vip"}, SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, tagged, 1)
	assert.Equal(t, uint(1), tagged[0].ID)

	found, total, err := repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Search: "C1", SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, found, 1)
	assert.Equal(t, uint(1), found[0].ID)

	// search 大小写不敏感（LOWER 语义）。
	found, total, err = repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, Search: "u1@E.COM", SortBy: "created_at", SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, found, 1)
	assert.Equal(t, uint(1), found[0].ID)
}

func TestCustomerRepoListCustomersCountError(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	seedCustomerListRows(t, db, time.Now())

	// 注入第 1 次 SELECT 失败（List 的 count 阶段），覆盖 count 错误分支。
	var calls int32
	_ = db.Callback().Query().Before("gorm:query").Register("fail_first_count", func(tx *gorm.DB) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_ = tx.AddError(errors.New("forced count failure"))
		}
	})
	_, _, err := repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to count customers")
}

func TestCustomerRepoListCustomersScanError(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	seedCustomerListRows(t, db, time.Now())

	// 非法排序表达式使 count 成功后的 scan 失败
	_, _, err := repo.ListCustomers(context.Background(), customerapp.ListCustomersQuery{
		Page: 1, PageSize: 20, SortBy: "id)", SortOrder: "desc",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list customers")
}

func TestCustomerRepoGetCustomerActivity(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "t1", "w1", now)

	require.NoError(t, db.Create(&[]models.Session{
		{ID: "s-old", UserID: 1, Status: "active", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "s-new", UserID: 1, Status: "active", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
		{ID: "s-other", UserID: 1, Status: "active", TenantID: "t1", WorkspaceID: "w2", CreatedAt: now, UpdatedAt: now},
	}).Error)
	agentID := uint(100)
	require.NoError(t, db.Create(&[]models.Ticket{
		{Title: "tk-old", CustomerID: 1, AgentID: &agentID, Status: "open", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{Title: "tk-other", CustomerID: 1, AgentID: &agentID, Status: "open", TenantID: "t1", WorkspaceID: "w2", CreatedAt: now, UpdatedAt: now},
	}).Error)
	require.NoError(t, db.Create(&[]models.Message{
		{SessionID: "s-old", UserID: 1, Type: "text", Content: "m1", TenantID: "t1", WorkspaceID: "w1", CreatedAt: now.Add(-3 * time.Hour)},
		{SessionID: "s-other", UserID: 1, Type: "text", Content: "m2", TenantID: "t1", WorkspaceID: "w2", CreatedAt: now},
	}).Error)

	scoped, err := repo.GetCustomerActivity(customerScopeCtx("t1", "w1"), 1, 10)
	require.NoError(t, err)
	assert.Equal(t, uint(1), scoped.CustomerID)
	require.Len(t, scoped.RecentSessions, 2)
	assert.Equal(t, "s-new", scoped.RecentSessions[0].ID)
	assert.Equal(t, "s-old", scoped.RecentSessions[1].ID)
	require.Len(t, scoped.RecentTickets, 1)
	assert.Equal(t, "tk-old", scoped.RecentTickets[0].Title)
	require.Len(t, scoped.RecentMessages, 1)
	assert.Equal(t, "m1", scoped.RecentMessages[0].Content)

	// limit 生效：只取最新一条
	limited, err := repo.GetCustomerActivity(customerScopeCtx("t1", "w1"), 1, 1)
	require.NoError(t, err)
	require.Len(t, limited.RecentSessions, 1)
	assert.Equal(t, "s-new", limited.RecentSessions[0].ID)

	unscoped, err := repo.GetCustomerActivity(context.Background(), 1, 10)
	require.NoError(t, err)
	assert.Len(t, unscoped.RecentSessions, 3)
	assert.Len(t, unscoped.RecentTickets, 2)
	assert.Len(t, unscoped.RecentMessages, 2)
}

func TestCustomerRepoAddNote(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "", "", now)

	err := repo.AddNote(context.Background(), 1, customerapp.CustomerNoteDTO{
		AuthorID: 7, Content: "hello", CreatedAt: now,
	})
	require.NoError(t, err)

	var stored models.Customer
	require.NoError(t, db.First(&stored, "user_id = ?", 1).Error)
	assert.Equal(t, "["+now.Format("2006-01-02 15:04:05")+"] 用户7: hello", stored.Notes)

	err = repo.AddNote(context.Background(), 1, customerapp.CustomerNoteDTO{
		AuthorID: 8, Content: "world", CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, db.First(&stored, "user_id = ?", 1).Error)
	lines := strings.Split(stored.Notes, "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], "用户8: world")

	err = repo.AddNote(context.Background(), 999, customerapp.CustomerNoteDTO{AuthorID: 7, Content: "x", CreatedAt: now})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")

	// 通过触发器强制 update 失败
	require.NoError(t, db.Exec("CREATE TRIGGER trg_notes BEFORE UPDATE OF notes ON customers BEGIN SELECT RAISE(ABORT, 'no notes'); END;").Error)
	err = repo.AddNote(context.Background(), 1, customerapp.CustomerNoteDTO{AuthorID: 7, Content: "boom", CreatedAt: now})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add note")
}

func TestCustomerRepoUpdateTags(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "", "", now)

	err := repo.UpdateTags(context.Background(), 1, []string{"vip", "gold"})
	require.NoError(t, err)
	var stored models.Customer
	require.NoError(t, db.First(&stored, "user_id = ?", 1).Error)
	assert.Equal(t, "vip,gold", stored.Tags)

	err = repo.UpdateTags(context.Background(), 999, []string{"vip"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))

	db2 := newCustomerRepoTestDB(t)
	require.NoError(t, db2.Migrator().DropTable(&models.Customer{}))
	err = NewGormRepository(db2).UpdateTags(context.Background(), 1, []string{"vip"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update tags")
}

func TestCustomerRepoGetStats(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()

	users := []models.User{
		{ID: 1, Username: "u1", Email: "u1@e.com", Role: "customer", Status: "active", LastLogin: &now, CreatedAt: now.AddDate(0, 0, -40), UpdatedAt: now},
		{ID: 2, Username: "u2", Email: "u2@e.com", Role: "customer", Status: "active", CreatedAt: now.AddDate(0, 0, -1), UpdatedAt: now},
		{ID: 3, Username: "u3", Email: "u3@e.com", Role: "customer", Status: "active", CreatedAt: now.AddDate(0, 0, -40), UpdatedAt: now},
		{ID: 4, Username: "agent", Email: "ag@e.com", Role: "agent", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&users).Error)
	customers := []models.Customer{
		{UserID: 1, TenantID: "t1", WorkspaceID: "w1", Company: "C1", Source: "web", Industry: "tech", Priority: "high", CreatedAt: now, UpdatedAt: now},
		{UserID: 2, TenantID: "t1", WorkspaceID: "w1", Company: "C2", Source: "referral", Industry: "", Priority: "normal", CreatedAt: now, UpdatedAt: now},
		{UserID: 3, TenantID: "t2", WorkspaceID: "w2", Company: "C3", Source: "web", Industry: "fin", Priority: "low", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&customers).Error)

	stats, err := repo.GetStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats.Total)
	assert.Equal(t, int64(1), stats.Active)
	assert.Equal(t, int64(1), stats.NewThisWeek)

	bySource := map[string]int64{}
	for _, s := range stats.BySource {
		bySource[s.Source] = s.Count
	}
	assert.Equal(t, map[string]int64{"web": 2, "referral": 1}, bySource)

	byIndustry := map[string]int64{}
	for _, s := range stats.ByIndustry {
		byIndustry[s.Industry] = s.Count
	}
	assert.Equal(t, map[string]int64{"tech": 1, "fin": 1}, byIndustry) // 空行业被 Having 过滤

	byPriority := map[string]int64{}
	for _, s := range stats.ByPriority {
		byPriority[s.Priority] = s.Count
	}
	assert.Equal(t, map[string]int64{"high": 1, "normal": 1, "low": 1}, byPriority)

	scoped, err := repo.GetStats(customerScopeCtx("t1", "w1"))
	require.NoError(t, err)
	assert.Equal(t, int64(2), scoped.Total)
	assert.Equal(t, int64(1), scoped.Active)
	assert.Equal(t, int64(1), scoped.NewThisWeek)
	require.Len(t, scoped.ByIndustry, 1)
	assert.Equal(t, "tech", scoped.ByIndustry[0].Industry)
}

func TestCustomerRepoRevokeCustomerTokens(t *testing.T) {
	db := newCustomerRepoTestDB(t)
	repo := NewGormRepository(db)
	now := time.Now()
	seedCustomerRow(t, db, 1, "alice", "", "", now)

	_, err := repo.RevokeCustomerTokens(context.Background(), 999, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer not found")

	version, err := repo.RevokeCustomerTokens(context.Background(), 1, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	var user models.User
	require.NoError(t, db.First(&user, 1).Error)
	require.NotNil(t, user.TokenValidAfter)
	assert.Equal(t, 1, user.TokenVersion)

	explicit := now.Add(time.Hour).UTC()
	version, err = repo.RevokeCustomerTokens(context.Background(), 1, explicit)
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	db2 := newCustomerRepoTestDB(t)
	seedCustomerRow(t, db2, 1, "alice", "", "", now)
	require.NoError(t, db2.Migrator().DropTable(&models.User{}))
	_, err = NewGormRepository(db2).RevokeCustomerTokens(context.Background(), 1, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to revoke customer tokens")
}

func TestApplyCustomerScopeFieldsDirect(t *testing.T) {
	applyCustomerScopeFields(customerScopeCtx("t1", "w1"), nil) // 不应 panic

	customer := &models.Customer{}
	applyCustomerScopeFields(customerScopeCtx("t9", "w9"), customer)
	assert.Equal(t, "t9", customer.TenantID)
	assert.Equal(t, "w9", customer.WorkspaceID)

	empty := &models.Customer{}
	applyCustomerScopeFields(context.Background(), empty)
	assert.Empty(t, empty.TenantID)
	assert.Empty(t, empty.WorkspaceID)
}
