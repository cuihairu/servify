package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureRepo struct {
	getID        uint
	getCalls     int
	updateID     uint
	lastUpdate   UpdateCustomerCommand
	listQuery    ListCustomersQuery
	activityID   uint
	activityLmt  int
	lastNoteID   uint
	lastNote     CustomerNoteDTO
	tagsID       uint
	lastTagsCall []string
	statsCalls   int
	revokeID     uint
	revokeAt     time.Time
	revokeCalls  int
}

func (c *captureRepo) CreateCustomer(ctx context.Context, cmd CreateCustomerCommand) (*models.User, error) {
	return &models.User{ID: 1, Username: cmd.Username, Role: "customer", Status: "active"}, nil
}

func (c *captureRepo) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	c.getCalls++
	c.getID = customerID
	return &models.User{ID: customerID, Username: "captured"}, nil
}

func (c *captureRepo) UpdateCustomer(ctx context.Context, customerID uint, cmd UpdateCustomerCommand) (*models.User, error) {
	c.updateID = customerID
	c.lastUpdate = cmd
	return &models.User{ID: customerID}, nil
}

func (c *captureRepo) ListCustomers(ctx context.Context, query ListCustomersQuery) ([]CustomerInfoDTO, int64, error) {
	c.listQuery = query
	return []CustomerInfoDTO{{User: models.User{ID: 7}, Company: "A Co"}}, 1, nil
}

func (c *captureRepo) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*CustomerActivityDTO, error) {
	c.activityID = customerID
	c.activityLmt = limit
	return &CustomerActivityDTO{CustomerID: customerID}, nil
}

func (c *captureRepo) AddNote(ctx context.Context, customerID uint, note CustomerNoteDTO) error {
	c.lastNoteID = customerID
	c.lastNote = note
	return nil
}

func (c *captureRepo) UpdateTags(ctx context.Context, customerID uint, tags []string) error {
	c.tagsID = customerID
	c.lastTagsCall = tags
	return nil
}

func (c *captureRepo) GetStats(ctx context.Context) (*CustomerStatsDTO, error) {
	c.statsCalls++
	return &CustomerStatsDTO{Total: 9}, nil
}

func (c *captureRepo) RevokeCustomerTokens(ctx context.Context, customerID uint, revokeAt time.Time) (int, error) {
	c.revokeCalls++
	c.revokeID = customerID
	c.revokeAt = revokeAt
	return 3, nil
}

func TestCreateCustomerRequiresUsername(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	for _, username := range []string{"", "   "} {
		_, err := svc.CreateCustomer(context.Background(), CreateCustomerCommand{Username: username, Email: "a@b.c"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "username required")
	}
}

func TestGetCustomerByIDDelegates(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	user, err := svc.GetCustomerByID(context.Background(), 5)
	require.NoError(t, err)
	assert.Equal(t, uint(5), user.ID)
	assert.Equal(t, 1, repo.getCalls)
	assert.Equal(t, uint(5), repo.getID)
}

func TestUpdateCustomerNormalizesTagPointer(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	tags := []string{" a ", "a", "b", ""}
	_, err := svc.UpdateCustomer(context.Background(), 4, UpdateCustomerCommand{Tags: &tags})
	require.NoError(t, err)
	require.NotNil(t, repo.lastUpdate.Tags)
	assert.Equal(t, []string{"a", "b"}, *repo.lastUpdate.Tags)
	assert.Equal(t, uint(4), repo.updateID)

	_, err = svc.UpdateCustomer(context.Background(), 4, UpdateCustomerCommand{})
	require.NoError(t, err)
	assert.Nil(t, repo.lastUpdate.Tags)
}

func TestListCustomersNormalizesQuery(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	items, total, err := svc.ListCustomers(context.Background(), ListCustomersQuery{
		Page:      -1,
		PageSize:  500,
		Tags:      []string{" x ", "x"},
		SortBy:    "hacked; drop",
		SortOrder: "ASC",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, items, 1)
	assert.Equal(t, "A Co", items[0].Company)
	assert.Equal(t, 1, repo.listQuery.Page)
	assert.Equal(t, 200, repo.listQuery.PageSize)
	assert.Equal(t, []string{"x"}, repo.listQuery.Tags)
	assert.Equal(t, "created_at", repo.listQuery.SortBy)
	assert.Equal(t, "asc", repo.listQuery.SortOrder)

	_, _, err = svc.ListCustomers(context.Background(), ListCustomersQuery{
		Page:      3,
		PageSize:  50,
		SortBy:    "email",
		SortOrder: "desc",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, repo.listQuery.Page)
	assert.Equal(t, 50, repo.listQuery.PageSize)
	assert.Equal(t, "email", repo.listQuery.SortBy)
	assert.Equal(t, "desc", repo.listQuery.SortOrder)
}

func TestGetCustomerActivityDefaultsLimit(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	for _, tc := range []struct {
		in   int
		want int
	}{{0, 10}, {-3, 10}, {7, 7}} {
		activity, err := svc.GetCustomerActivity(context.Background(), 6, tc.in)
		require.NoError(t, err)
		assert.Equal(t, uint(6), activity.CustomerID)
		assert.Equal(t, tc.want, repo.activityLmt)
	}
}

func TestAddNoteValidationAndPassthrough(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	err := svc.AddNote(context.Background(), 1, "", 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "note required")

	err = svc.AddNote(context.Background(), 1, "   ", 2)
	require.Error(t, err)

	before := time.Now()
	err = svc.AddNote(context.Background(), 3, "  hello  ", 8)
	require.NoError(t, err)
	assert.Equal(t, uint(3), repo.lastNoteID)
	assert.Equal(t, "hello", repo.lastNote.Content)
	assert.Equal(t, uint(8), repo.lastNote.AuthorID)
	assert.False(t, repo.lastNote.CreatedAt.Before(before))
}

func TestUpdateTagsDelegates(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	err := svc.UpdateTags(context.Background(), 2, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, uint(2), repo.tagsID)
	assert.Equal(t, []string{"a"}, repo.lastTagsCall)
}

func TestGetStatsDelegates(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	stats, err := svc.GetStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(9), stats.Total)
	assert.Equal(t, 1, repo.statsCalls)
}

func TestRevokeCustomerTokensValidation(t *testing.T) {
	repo := &captureRepo{}
	svc := NewService(repo)

	_, err := svc.RevokeCustomerTokens(context.Background(), 0, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customer_id required")
	assert.Equal(t, 0, repo.revokeCalls)

	before := time.Now()
	version, err := svc.RevokeCustomerTokens(context.Background(), 5, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 3, version)
	assert.Equal(t, uint(5), repo.revokeID)
	assert.False(t, repo.revokeAt.Before(before.UTC().Add(-time.Minute)))
	assert.True(t, repo.revokeAt.After(before.UTC().Add(-time.Minute)))

	explicit := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err = svc.RevokeCustomerTokens(context.Background(), 6, explicit)
	require.NoError(t, err)
	assert.True(t, repo.revokeAt.Equal(explicit))
}

func TestNormalizeHelpers(t *testing.T) {
	assert.True(t, strings.TrimSpace("") == "")
	assert.Equal(t, []string{}, normalizeTags(nil))
	assert.Equal(t, []string{"a"}, normalizeTags([]string{"", "  ", "a", "a"}))
	assert.Equal(t, 1, normalizePage(0))
	assert.Equal(t, 1, normalizePage(-2))
	assert.Equal(t, 4, normalizePage(4))
	assert.Equal(t, 20, normalizePageSize(0))
	assert.Equal(t, 20, normalizePageSize(-1))
	assert.Equal(t, 200, normalizePageSize(201))
	assert.Equal(t, 50, normalizePageSize(50))
	for _, valid := range []string{"created_at", "updated_at", "name", "email", "status"} {
		assert.Equal(t, valid, normalizeSortBy(valid))
	}
	assert.Equal(t, "created_at", normalizeSortBy("id"))
	assert.Equal(t, "created_at", normalizeSortBy(""))
	assert.Equal(t, "asc", normalizeSortOrder("ASC"))
	assert.Equal(t, "asc", normalizeSortOrder("asc"))
	assert.Equal(t, "desc", normalizeSortOrder(""))
	assert.Equal(t, "desc", normalizeSortOrder("bogus"))
}

type failingListRepo struct {
	captureRepo
	err error
}

func (f *failingListRepo) ListCustomers(ctx context.Context, query ListCustomersQuery) ([]CustomerInfoDTO, int64, error) {
	return nil, 0, f.err
}

func TestListCustomersPropagatesRepoError(t *testing.T) {
	// 等价覆盖自 legacy services.TestCustomerService_ModuleQueryError（facade 删除后迁入）：
	// 归一化之后仍透传 repo 错误。
	svc := NewService(&failingListRepo{err: errors.New("list boom")})
	_, _, err := svc.ListCustomers(context.Background(), ListCustomersQuery{Page: 2, PageSize: 10})
	require.Error(t, err)
	assert.Equal(t, "list boom", err.Error())
}
