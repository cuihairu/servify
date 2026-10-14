package infra

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	platformauth "servify/apps/server/internal/platform/auth"
)

// newTranslateAwareDB 复刻 bootstrap.OpenDB 的连接形态：生产走同一个 gorm.Config
// 开关（TranslateError），否则 driver 原始约束错误不会被折成 gorm 哨兵。
func newTranslateAwareDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newAgentGroupTestDB(t)
	// 直接改配置模拟生产 OpenDB 的 TranslateError 开关（AutoMigrate 已完成，
	// 开关只影响后续查询/写入的错误翻译）。
	db.TranslateError = true
	return db
}

// TestAgentGroupDuplicateMapsToSentinel 钉住「同名组 → ErrGroupDuplicate」。
//
// 回归背景：agent/application/groups.go 用 errors.Is(err, gorm.ErrDuplicatedKey)
// 判重，但生产 OpenDB 从未开启 gorm.Config.TranslateError，driver 返回的是
// 「UNIQUE constraint failed: ...」原文，errors.Is 恒 false → 唯一键冲突从
// 409 Conflict 退化成 500 Internal Server Error。
// 本测试先经真实 sqlite 写入制造冲突，确认 infra 抛的是应用层哨兵而非驱动原文。
func TestAgentGroupDuplicateMapsToSentinel(t *testing.T) {
	repo := NewGormRepository(newTranslateAwareDB(t))
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	if err := repo.CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"}); err != nil {
		t.Fatalf("first CreateAgentGroup() error = %v", err)
	}

	dupErr := repo.CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"})
	if !errors.Is(dupErr, agentapp.ErrGroupDuplicate) {
		t.Fatalf("CreateAgentGroup duplicate want ErrGroupDuplicate, got %v", dupErr)
	}

	// 跨工作区同名不算冲突（唯一索引含 workspace_id），哨兵不得误报。
	other := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-b")
	if err := repo.CreateAgentGroup(other, &models.AgentGroup{Name: "billing"}); err != nil {
		t.Fatalf("cross-workspace CreateAgentGroup() error = %v", err)
	}
}

// TestUpdateAgentGroupDuplicateMapsToSentinel 覆盖改名撞名的更新路径。
func TestUpdateAgentGroupDuplicateMapsToSentinel(t *testing.T) {
	repo := NewGormRepository(newTranslateAwareDB(t))
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	billing := &models.AgentGroup{Name: "billing"}
	if err := repo.CreateAgentGroup(ctx, billing); err != nil {
		t.Fatalf("create billing: %v", err)
	}
	sales := &models.AgentGroup{Name: "sales"}
	if err := repo.CreateAgentGroup(ctx, sales); err != nil {
		t.Fatalf("create sales: %v", err)
	}

	sales.Name = "billing" // 撞上 billing 的唯一键
	if err := repo.UpdateAgentGroup(ctx, sales); !errors.Is(err, agentapp.ErrGroupDuplicate) {
		t.Fatalf("UpdateAgentGroup duplicate want ErrGroupDuplicate, got %v", err)
	}

	// 改成自身不冲突的名字必须成功（防止哨兵映射把正常更新一并吞掉）。
	sales.Name = "sales-v2"
	if err := repo.UpdateAgentGroup(ctx, sales); err != nil {
		t.Fatalf("non-conflicting UpdateAgentGroup() error = %v", err)
	}
}

// TestAgentGroupNonDuplicateErrorsUnaffected 钉住「非唯一键错误不被哨兵吞掉」：
// 丢表后 Create/Update 应保留原始驱动错误，便于排障。
func TestAgentGroupNonDuplicateErrorsUnaffected(t *testing.T) {
	db := newTranslateAwareDB(t)
	db.TranslateError = true
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-a")

	if err := db.Migrator().DropTable(&models.AgentGroup{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if err := repo.CreateAgentGroup(ctx, &models.AgentGroup{Name: "billing"}); err == nil {
		t.Fatal("CreateAgentGroup on missing table want error, got nil")
	} else if errors.Is(err, agentapp.ErrGroupDuplicate) {
		t.Fatalf("missing-table error must not map to ErrGroupDuplicate, got %v", err)
	}
	if err := repo.UpdateAgentGroup(ctx, &models.AgentGroup{ID: 1, Name: "x"}); err == nil {
		t.Fatal("UpdateAgentGroup on missing table want error, got nil")
	} else if errors.Is(err, agentapp.ErrGroupDuplicate) {
		t.Fatalf("missing-table error must not map to ErrGroupDuplicate, got %v", err)
	}
}

var _ = sqlite.Open
