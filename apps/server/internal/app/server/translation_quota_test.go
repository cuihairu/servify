package server

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	translationdelivery "servify/apps/server/internal/modules/translation/delivery"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func quotaTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func quotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:quota_scope_"+t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TenantConfig{}, &models.WorkspaceConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestBuildTranslationQuotaGateDisabledOrBare(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	logger := logrus.New()

	// quota 关闭：原样返回（零行为）。
	cfg := &config.Config{}
	got, err := buildTranslationQuotaGate(cfg, nil, nil, next, logger)
	if err != nil || got != next {
		t.Fatalf("disabled quota = (%v, %v), want bare next", got, err)
	}
	// cfg 为 nil / next 为 nil：同样原样返回。
	if got, err := buildTranslationQuotaGate(nil, nil, nil, next, logger); err != nil || got != next {
		t.Fatalf("nil cfg = (%v, %v), want bare next", got, err)
	}
	if got, err := buildTranslationQuotaGate(cfg, nil, nil, nil, logger); err != nil || got != nil {
		t.Fatalf("nil next = (%v, %v), want nil", got, err)
	}
}

func TestBuildTranslationQuotaGateValidation(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	logger := logrus.New()
	client := quotaTestRedis(t)

	cases := []struct {
		name    string
		unit    string
		win     string
		limit   int64
		redis   bool
		wantSub string
	}{
		{"bad unit", "chars", "day", 100, true, "translation.quota.unit"},
		{"bad window", "tokens", "week", 100, true, "translation.quota.window"},
		{"zero limit", "tokens", "day", 0, true, "translation.quota.limit"},
		{"negative limit", "requests", "hour", -1, true, "translation.quota.limit"},
		{"missing redis", "tokens", "day", 100, false, "requires redis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Translation.Quota = config.TranslationQuotaConfig{
				Enabled: true, Limit: tc.limit, Unit: tc.unit, Window: tc.win,
			}
			rc := client
			if !tc.redis {
				rc = nil
			}
			got, err := buildTranslationQuotaGate(cfg, nil, rc, next, logger)
			if err == nil || got != nil {
				t.Fatalf("expected startup error, got (%v, %v)", got, err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q must mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestBuildTranslationQuotaGateEnabledWraps(t *testing.T) {
	next := translationdelivery.NewTranslationHandlerService(nil, translationdelivery.RuntimeParams{})
	cfg := &config.Config{}
	cfg.Translation.Quota = config.TranslationQuotaConfig{
		Enabled: true, Limit: 42, Unit: "requests", Window: "day",
	}
	got, err := buildTranslationQuotaGate(cfg, quotaTestDB(t), quotaTestRedis(t), next, logrus.New())
	if err != nil {
		t.Fatalf("valid config must assemble: %v", err)
	}
	if got == nil || got == next {
		t.Fatal("enabled quota must wrap next with a gate")
	}
}

// TestNewTranslationQuotaLimitSource 限额解析适配器：db 为 nil 返回 nil
// （静态系统限额）；有主库时按 ctx 租户/工作区覆盖解析，未覆盖回落系统值。
func TestNewTranslationQuotaLimitSource(t *testing.T) {
	cfg := &config.Config{}
	cfg.Translation.Quota.Limit = 100

	if src := newTranslationQuotaLimitSource(cfg, nil); src != nil {
		t.Fatalf("nil db must yield nil source (static limit only), got %v", src)
	}

	db := quotaTestDB(t)
	if err := db.Create(&models.TenantConfig{
		TenantID:             "tenant-a",
		TranslationQuotaJSON: `{"limit":7}`,
	}).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	src := newTranslationQuotaLimitSource(cfg, db)
	if src == nil {
		t.Fatal("db-backed source must be wired")
	}
	if got := src(platformauth.ContextWithScope(context.Background(), "tenant-a", "")); got != 7 {
		t.Fatalf("tenant-a override limit = %d, want 7", got)
	}
	if got := src(platformauth.ContextWithScope(context.Background(), "tenant-b", "")); got != 100 {
		t.Fatalf("tenant-b fallback limit = %d, want 100 (system default)", got)
	}
	if err := db.Create(&models.WorkspaceConfig{
		TenantID:             "tenant-a",
		WorkspaceID:          "ws-1",
		TranslationQuotaJSON: `{"limit":9}`,
	}).Error; err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if got := src(platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")); got != 9 {
		t.Fatalf("workspace override limit = %d, want 9", got)
	}
}
