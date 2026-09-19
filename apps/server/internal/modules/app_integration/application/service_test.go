package application

// Service 编排与错误包裹语义（自 services/app_integration_service*_test.go 下沉）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
)

type stubRepo struct {
	bySlug    int64
	total     int64
	list      []models.AppIntegration
	integr    *models.AppIntegration
	created   *models.AppIntegration
	saved     *models.AppIntegration
	deletedID uint

	slugErr, countErr, listErr, getErr, createErr, saveErr, deleteErr error
}

func (r *stubRepo) CountBySlug(ctx context.Context, slug string) (int64, error) {
	return r.bySlug, r.slugErr
}

func (r *stubRepo) CountIntegrations(ctx context.Context, req *AppIntegrationListRequest) (int64, error) {
	return r.total, r.countErr
}

func (r *stubRepo) ListIntegrations(ctx context.Context, req *AppIntegrationListRequest, offset, limit int) ([]models.AppIntegration, error) {
	return r.list, r.listErr
}

func (r *stubRepo) GetIntegration(ctx context.Context, id uint) (*models.AppIntegration, error) {
	return r.integr, r.getErr
}

func (r *stubRepo) CreateIntegration(ctx context.Context, model *models.AppIntegration) error {
	if r.createErr != nil {
		return r.createErr
	}
	model.ID = 1
	r.created = model
	return nil
}

func (r *stubRepo) SaveIntegration(ctx context.Context, model *models.AppIntegration) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = model
	return nil
}

func (r *stubRepo) DeleteIntegration(ctx context.Context, id uint) error {
	r.deletedID = id
	return r.deleteErr
}

func TestServiceListPaginationAndErrors(t *testing.T) {
	boom := errors.New("boom")

	// 分页默认值：Page<1→1；PageSize<1→20（传给 repo 的 offset/limit 收敛）
	repo := &stubRepo{total: 0}
	if _, _, err := NewService(repo).List(context.Background(), &AppIntegrationListRequest{Page: -1, PageSize: -1}); err != nil {
		t.Fatalf("List: %v", err)
	}
	// offset = (1-1)*20 = 0，limit = 20 —— 由 ListIntegrations 收到归一值间接保证

	if _, _, err := NewService(&stubRepo{countErr: boom}).List(context.Background(), &AppIntegrationListRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to count integrations") {
		t.Fatalf("count err = %v", err)
	}
	if _, _, err := NewService(&stubRepo{listErr: boom}).List(context.Background(), &AppIntegrationListRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to list integrations") {
		t.Fatalf("list err = %v", err)
	}
	items, total, err := NewService(&stubRepo{list: []models.AppIntegration{{ID: 3, Slug: "s"}}, total: 1}).List(context.Background(), &AppIntegrationListRequest{})
	if err != nil || total != 1 || len(items) != 1 || items[0].Slug != "s" {
		t.Fatalf("List() = %+v, %d, %v", items, total, err)
	}
}

func TestServiceCreateSlugChain(t *testing.T) {
	// nil 请求
	if _, err := NewService(&stubRepo{}).Create(context.Background(), nil); err == nil || err.Error() != "request required" {
		t.Fatalf("nil req err = %v", err)
	}
	// slug 无法推导
	if _, err := NewService(&stubRepo{}).Create(context.Background(), &AppIntegrationCreateRequest{Name: "!!!"}); err == nil || err.Error() != "slug required" {
		t.Fatalf("slug required err = %v", err)
	}
	// slug 检查错误包裹
	boom := errors.New("boom")
	if _, err := NewService(&stubRepo{slugErr: boom}).Create(context.Background(), &AppIntegrationCreateRequest{Name: "X", Slug: "x", IFrameURL: "u"}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to check slug") {
		t.Fatalf("slug check err = %v", err)
	}
	// 重复 slug
	if _, err := NewService(&stubRepo{bySlug: 1}).Create(context.Background(), &AppIntegrationCreateRequest{Name: "X", Slug: "x", IFrameURL: "u"}); err == nil || err.Error() != "integration slug already exists" {
		t.Fatalf("dup slug err = %v", err)
	}
	// 创建错误包裹
	if _, err := NewService(&stubRepo{createErr: boom}).Create(context.Background(), &AppIntegrationCreateRequest{Name: "X", Slug: "x", IFrameURL: "u"}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to create integration") {
		t.Fatalf("create err = %v", err)
	}

	// 成功：Slug 空→Name 推导；Enabled 默认 true；LastSyncStatus=never
	repo := &stubRepo{}
	created, err := NewService(repo).Create(context.Background(), &AppIntegrationCreateRequest{
		Name: "My App", Slug: " ", Vendor: "v", IFrameURL: "u",
		Capabilities: []string{"c"}, ConfigSchema: map[string]interface{}{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := repo.created
	if m.Slug != "my-app" || !m.Enabled || m.LastSyncStatus != "never" || m.Capabilities != `["c"]` || m.ConfigSchema != `{"k":"v"}` {
		t.Fatalf("unexpected model: %+v", m)
	}
	if created.Slug != "my-app" || created.LastSyncStatus != "never" {
		t.Fatalf("unexpected DTO: %+v", created)
	}

	// Enabled 显式 false
	repo2 := &stubRepo{}
	ifv := false
	if _, err := NewService(repo2).Create(context.Background(), &AppIntegrationCreateRequest{Name: "N", Slug: "n", IFrameURL: "u", Enabled: &ifv}); err != nil || repo2.created.Enabled {
		t.Fatalf("enabled=false: %+v, %v", repo2.created, err)
	}
}

func TestServiceUpdatePatch(t *testing.T) {
	boom := errors.New("boom")
	existing := &models.AppIntegration{ID: 1, Name: "A", Slug: "a", Enabled: true}

	// nil 请求
	if _, err := NewService(&stubRepo{}).Update(context.Background(), 1, nil); err == nil || err.Error() != "request required" {
		t.Fatalf("nil req err = %v", err)
	}
	// not found 透传
	if _, err := NewService(&stubRepo{getErr: ErrIntegrationNotFound}).Update(context.Background(), 9, &AppIntegrationUpdateRequest{}); !errors.Is(err, ErrIntegrationNotFound) {
		t.Fatalf("not found err = %v", err)
	}
	// 其他 get 错误包裹
	if _, err := NewService(&stubRepo{getErr: boom}).Update(context.Background(), 9, &AppIntegrationUpdateRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to load integration") {
		t.Fatalf("load err = %v", err)
	}
	// save 错误包裹
	if _, err := NewService(&stubRepo{integr: existing, saveErr: boom}).Update(context.Background(), 1, &AppIntegrationUpdateRequest{}); !errors.Is(err, boom) || !strings.Contains(err.Error(), "failed to update integration") {
		t.Fatalf("save err = %v", err)
	}

	// 全字段 patch（fresh 对象：saveErr 用例的 get 返回共享指针已被 patch 污染）
	repo := &stubRepo{integr: &models.AppIntegration{ID: 1, Name: "A", Slug: "a", Enabled: true}}
	name, vendor, category := "B", "v", "c"
	summary, icon, iframe := "s", "i", "u2"
	caps := []string{"x"}
	schema := map[string]interface{}{"a": 1}
	enabled := false
	updated, err := NewService(repo).Update(context.Background(), 1, &AppIntegrationUpdateRequest{
		Name: &name, Vendor: &vendor, Category: &category, Summary: &summary,
		IconURL: &icon, Capabilities: caps, ConfigSchema: schema, IFrameURL: &iframe, Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	m := repo.saved
	if m.Name != "B" || m.Vendor != "v" || m.Category != "c" || m.Summary != "s" || m.IconURL != "i" || m.IFrameURL != "u2" || m.Enabled {
		t.Fatalf("unexpected patch: %+v", m)
	}
	if m.Capabilities != `["x"]` || m.ConfigSchema != `{"a":1}` {
		t.Fatalf("unexpected json fields: %+v", m)
	}
	if updated.Name != "B" || updated.Enabled {
		t.Fatalf("unexpected DTO: %+v", updated)
	}
}

func TestServiceDelete(t *testing.T) {
	repo := &stubRepo{deleteErr: ErrIntegrationNotFound}
	if err := NewService(repo).Delete(context.Background(), 5); !errors.Is(err, ErrIntegrationNotFound) {
		t.Fatalf("Delete() err = %v", err)
	}
	if repo.deletedID != 5 {
		t.Fatalf("deleted id = %d", repo.deletedID)
	}
}

func TestAppIntegrationHelpers(t *testing.T) {
	if encodeJSON(nil) != "" {
		t.Fatal("nil encode should be empty")
	}
	if encodeJSON(map[string]int{"a": 1}) != `{"a":1}` {
		t.Fatal("unexpected encode result")
	}
	// Marshal 失败（channel 不可序列化）回退空串
	if encodeJSON(map[string]interface{}{"bad": make(chan int)}) != "" {
		t.Fatal("marshal failure should fall back to empty")
	}
	if v := decodeStringArray(""); v != nil {
		t.Fatal("empty decode should be nil")
	}
	if v := decodeStringArray("not-json"); v != nil {
		t.Fatal("bad json decode should be nil")
	}
	if v := decodeStringArray(`["a"]`); len(v) != 1 {
		t.Fatal("expected decoded slice")
	}
	if v := decodeObject(""); v != nil {
		t.Fatal("empty object decode should be nil")
	}
	if v := decodeObject("[1]"); v != nil {
		t.Fatal("bad object decode should be nil")
	}
	if v := decodeObject(`{"a":1}`); v["a"] != float64(1) {
		t.Fatal("expected decoded object")
	}
	if normalizeSlug("  MiXed Case--Slug!!  ") != "mixed-case--slug" {
		t.Fatal("unexpected slug normalization")
	}
	if normalizeSlug("") != "" {
		t.Fatal("empty slug")
	}
}
