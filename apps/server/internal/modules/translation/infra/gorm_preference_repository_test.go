package infra

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	translationapp "servify/apps/server/internal/modules/translation/application"
	translationdomain "servify/apps/server/internal/modules/translation/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var prefMemDBSeq atomic.Uint32

func newPrefUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := "file:transpref_" + name + "_" + strconv.FormatUint(uint64(prefMemDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&translationdomain.TranslationLanguagePreference{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&translationdomain.TranslationLanguagePreference{})
		_ = sqlDB.Close()
	})
	return db
}

func scopedCtx(tenant, workspace string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenant, workspace)
}

func upsertPref(t *testing.T, repo *GormPreferenceRepository, ctx context.Context, sessionID, viewerRole, lang string) {
	t.Helper()
	err := repo.UpsertPreference(ctx, &translationdomain.TranslationLanguagePreference{
		TenantID:              platformauth.TenantIDFromContext(ctx),
		WorkspaceID:           platformauth.WorkspaceIDFromContext(ctx),
		ConversationSessionID: sessionID,
		ViewerRole:            viewerRole,
		TargetLang:            lang,
	})
	if err != nil {
		t.Fatalf("UpsertPreference(%s/%s): %v", sessionID, viewerRole, err)
	}
}

// TestPreferenceUpsertCreateThenUpdate 同会话同读向二次 upsert 走原行更新
// （保 id/created_at，不新插行）。
func TestPreferenceUpsertCreateThenUpdate(t *testing.T) {
	db := newPrefUnitTestDB(t)
	repo := NewGormPreferenceRepository(db)
	ctx := scopedCtx("tenant-a", "ws-1")

	upsertPref(t, repo, ctx, "conv-1", translationapp.ViewerRoleAgent, "en")
	first, err := repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleAgent)
	if err != nil || first == nil {
		t.Fatalf("first get: %v %+v", err, first)
	}

	upsertPref(t, repo, ctx, "conv-1", translationapp.ViewerRoleAgent, "fr")
	second, err := repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleAgent)
	if err != nil || second == nil {
		t.Fatalf("second get: %v %+v", err, second)
	}
	if second.ID != first.ID {
		t.Fatalf("update must keep the same row: first=%d second=%d", first.ID, second.ID)
	}
	if second.TargetLang != "fr" {
		t.Fatalf("lang must be overwritten, got %q", second.TargetLang)
	}

	var count int64
	if err := db.Model(&translationdomain.TranslationLanguagePreference{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("want single row, got %d", count)
	}
}

// TestPreferenceViewerRolesAreIndependent 刀三角色维度核心语义：同一会话
// agent/visitor 两个读向各自一条偏好——互相读不到、互不覆盖、互不删除。
func TestPreferenceViewerRolesAreIndependent(t *testing.T) {
	db := newPrefUnitTestDB(t)
	repo := NewGormPreferenceRepository(db)
	ctx := scopedCtx("tenant-a", "ws-1")

	upsertPref(t, repo, ctx, "conv-1", translationapp.ViewerRoleAgent, "en")
	upsertPref(t, repo, ctx, "conv-1", translationapp.ViewerRoleVisitor, "ja")

	agentPref, err := repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleAgent)
	if err != nil || agentPref == nil || agentPref.TargetLang != "en" {
		t.Fatalf("agent pref: %v %+v", err, agentPref)
	}
	visitorPref, err := repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleVisitor)
	if err != nil || visitorPref == nil || visitorPref.TargetLang != "ja" {
		t.Fatalf("visitor pref: %v %+v", err, visitorPref)
	}
	if agentPref.ID == visitorPref.ID {
		t.Fatalf("roles must live on separate rows, both=%d", agentPref.ID)
	}

	// 覆盖一读向不动另一读向。
	upsertPref(t, repo, ctx, "conv-1", translationapp.ViewerRoleVisitor, "ko")
	agentPref, _ = repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleAgent)
	if agentPref == nil || agentPref.TargetLang != "en" {
		t.Fatalf("agent pref must survive visitor upsert, got %+v", agentPref)
	}
	var count int64
	if err := db.Model(&translationdomain.TranslationLanguagePreference{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("want two rows after visitor overwrite, got %d", count)
	}

	// 删除一读向保留另一读向。
	if err := repo.DeletePreference(ctx, "conv-1", translationapp.ViewerRoleVisitor); err != nil {
		t.Fatalf("delete visitor pref: %v", err)
	}
	gone, err := repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleVisitor)
	if err != nil || gone != nil {
		t.Fatalf("visitor pref must be gone, got %+v %v", gone, err)
	}
	agentPref, err = repo.GetPreference(ctx, "conv-1", translationapp.ViewerRoleAgent)
	if err != nil || agentPref == nil {
		t.Fatalf("agent pref must survive visitor delete, got %+v %v", agentPref, err)
	}
}

// TestPreferenceScopeIsolation 偏好面 scope 隔离（RA-1 同口径）：跨 scope
// 读=不存在；跨 scope 写拒绝（冲突错误），不触碰他租户行；无 scope 不过滤。
func TestPreferenceScopeIsolation(t *testing.T) {
	db := newPrefUnitTestDB(t)
	repo := NewGormPreferenceRepository(db)
	owner := scopedCtx("tenant-a", "ws-1")

	upsertPref(t, repo, owner, "conv-shared", translationapp.ViewerRoleAgent, "en")

	t.Run("cross tenant read is not found", func(t *testing.T) {
		got, err := repo.GetPreference(scopedCtx("tenant-b", "ws-1"), "conv-shared", translationapp.ViewerRoleAgent)
		if err != nil || got != nil {
			t.Fatalf("cross-tenant read must be nil/nil, got %+v %v", got, err)
		}
	})

	t.Run("cross tenant upsert conflicts without touching owner row", func(t *testing.T) {
		err := repo.UpsertPreference(scopedCtx("tenant-b", "ws-1"), &translationdomain.TranslationLanguagePreference{
			TenantID:              "tenant-b",
			WorkspaceID:           "ws-1",
			ConversationSessionID: "conv-shared",
			ViewerRole:            translationapp.ViewerRoleAgent,
			TargetLang:            "fr",
		})
		if !errors.Is(err, translationapp.ErrTranslationPrefConflict) {
			t.Fatalf("want ErrTranslationPrefConflict, got %v", err)
		}
		owned, err := repo.GetPreference(owner, "conv-shared", translationapp.ViewerRoleAgent)
		if err != nil || owned == nil || owned.TargetLang != "en" || owned.TenantID != "tenant-a" {
			t.Fatalf("owner row must be untouched, got %+v %v", owned, err)
		}
	})

	t.Run("cross tenant delete does not remove owner row", func(t *testing.T) {
		if err := repo.DeletePreference(scopedCtx("tenant-b", "ws-1"), "conv-shared", translationapp.ViewerRoleAgent); err != nil {
			t.Fatalf("cross-tenant delete: %v", err)
		}
		owned, err := repo.GetPreference(owner, "conv-shared", translationapp.ViewerRoleAgent)
		if err != nil || owned == nil {
			t.Fatalf("owner row must survive cross-tenant delete, got %+v %v", owned, err)
		}
	})

	t.Run("empty scope sees all rows (local dev semantics)", func(t *testing.T) {
		got, err := repo.GetPreference(context.Background(), "conv-shared", translationapp.ViewerRoleAgent)
		if err != nil || got == nil {
			t.Fatalf("empty scope must not filter, got %+v %v", got, err)
		}
	})
}

// TestPreferenceDeleteIdempotent 无行删除同样成功（不回显存在性）。
func TestPreferenceDeleteIdempotent(t *testing.T) {
	db := newPrefUnitTestDB(t)
	repo := NewGormPreferenceRepository(db)
	ctx := scopedCtx("tenant-a", "ws-1")

	upsertPref(t, repo, ctx, "conv-del", translationapp.ViewerRoleAgent, "en")
	if err := repo.DeletePreference(ctx, "conv-del", translationapp.ViewerRoleAgent); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.DeletePreference(ctx, "conv-del", translationapp.ViewerRoleAgent); err != nil {
		t.Fatalf("second delete must stay successful, got %v", err)
	}
	got, err := repo.GetPreference(ctx, "conv-del", translationapp.ViewerRoleAgent)
	if err != nil || got != nil {
		t.Fatalf("row must be gone, got %+v %v", got, err)
	}
}

// TestPreferenceUpsertErrorAndRacePaths upsert 的存储错误透传与复查覆盖路径：
// 首读错误、Create 错误、复查错误原样上抛；复查发现竞争行（语言不同）时
// 覆盖为本方语言。
func TestPreferenceUpsertErrorAndRacePaths(t *testing.T) {
	pref := func(sessionID string) *translationdomain.TranslationLanguagePreference {
		return &translationdomain.TranslationLanguagePreference{
			ConversationSessionID: sessionID,
			ViewerRole:            translationapp.ViewerRoleAgent,
			TargetLang:            "en",
		}
	}

	t.Run("first read error passes through", func(t *testing.T) {
		db := newPrefUnitTestDB(t)
		sqlDB, _ := db.DB()
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		repo := NewGormPreferenceRepository(db)
		if err := repo.UpsertPreference(context.Background(), pref("conv-1")); err == nil {
			t.Fatal("want storage error from first read")
		}
	})

	t.Run("create error passes through", func(t *testing.T) {
		db := newPrefUnitTestDB(t)
		if err := db.Callback().Create().After("gorm:create").Register("inject_create_error", func(tx *gorm.DB) {
			tx.AddError(errors.New("create exploded"))
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		repo := NewGormPreferenceRepository(db)
		err := repo.UpsertPreference(context.Background(), pref("conv-1"))
		if err == nil || !strings.Contains(err.Error(), "create exploded") {
			t.Fatalf("want create error passthrough, got %v", err)
		}
	})

	t.Run("recheck error passes through", func(t *testing.T) {
		db := newPrefUnitTestDB(t)
		sqlDB, _ := db.DB()
		if err := db.Callback().Create().After("gorm:create").Register("close_after_create", func(tx *gorm.DB) {
			_ = sqlDB.Close()
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		repo := NewGormPreferenceRepository(db)
		if err := repo.UpsertPreference(context.Background(), pref("conv-1")); err == nil {
			t.Fatal("want storage error from post-create recheck")
		}
	})

	t.Run("raced row with different lang is overwritten", func(t *testing.T) {
		db := newPrefUnitTestDB(t)
		// 模拟并发：Create 落库后、复查前，同会话同读向行被改写为他语言。
		if err := db.Callback().Create().After("gorm:create").Register("simul raced write", func(tx *gorm.DB) {
			tx.Exec("UPDATE translation_language_preferences SET target_lang = 'zz' WHERE conversation_session_id = 'conv-race' AND viewer_role = 'agent'")
		}); err != nil {
			t.Fatalf("register callback: %v", err)
		}
		repo := NewGormPreferenceRepository(db)
		ctx := scopedCtx("tenant-a", "ws-1")
		if err := repo.UpsertPreference(ctx, &translationdomain.TranslationLanguagePreference{
			TenantID:              "tenant-a",
			WorkspaceID:           "ws-1",
			ConversationSessionID: "conv-race",
			ViewerRole:            translationapp.ViewerRoleAgent,
			TargetLang:            "en",
		}); err != nil {
			t.Fatalf("upsert with raced row: %v", err)
		}
		got, err := repo.GetPreference(ctx, "conv-race", translationapp.ViewerRoleAgent)
		if err != nil || got == nil {
			t.Fatalf("get after race recovery: %v %+v", err, got)
		}
		if got.TargetLang != "en" {
			t.Fatalf("raced lang must be overwritten to en, got %q", got.TargetLang)
		}
	})
}

// TestPreferenceGetPassesThroughRawErrors 非 not-found 的存储错误原样上抛。
func TestPreferenceGetPassesThroughRawErrors(t *testing.T) {
	db := newPrefUnitTestDB(t)
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	repo := NewGormPreferenceRepository(db)
	_, err := repo.GetPreference(context.Background(), "conv-1", translationapp.ViewerRoleAgent)
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("want raw (non not-found) error, got %v", err)
	}
}
