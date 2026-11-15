package infra

// 首个管理员引导原子占位（Codex 深审 Critical-1/2 修复）的仓储语义测试。
// 并发互斥由占位表主键唯一性承担（两个方言一致），这里以确定性分支覆盖：
// 占位成功 / 占位已被消费 / 窗口内已有用户落库回滚 / 插入失败释放占位 /
// 计数失败 fail-closed。真·多连接并行不加测——sqlite 内存库的 BUSY 时序
// 不可确定性重放，PK 唯一性本身即互斥保证。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	slapp "servify/apps/server/internal/modules/auth/application"

	"gorm.io/gorm"
)

func bootstrapTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newServicesTestDB(t, &models.User{}, &models.UserAuthSession{}, &models.AuthBootstrapClaim{})
}

func bootstrapAdminInput() slapp.RegisterInput {
	return slapp.RegisterInput{
		Username: "root", Email: "root@x.com", Password: "pw123456", Role: "admin",
	}
}

// 占位成功：claimed=true、角色 admin、占位行落库。
func TestCreateBootstrapAdminClaimsOnce(t *testing.T) {
	db := bootstrapTestDB(t)
	repo := NewGormRepository(db)

	user := &models.User{Username: "root", Email: "root@x.com", Password: "x", Status: "active"}
	claimed, err := repo.CreateBootstrapAdmin(context.Background(), user)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	if user.Role != "admin" {
		t.Fatalf("role=%q want admin", user.Role)
	}
	var claims int64
	if err := db.Model(&models.AuthBootstrapClaim{}).Count(&claims).Error; err != nil || claims != 1 {
		t.Fatalf("claim rows=%d err=%v", claims, err)
	}

	// 第二次：已有用户落库 → 让位（回滚占位分支），不产生第二个 admin
	user2 := &models.User{Username: "root2", Email: "root2@x.com", Password: "x", Status: "active"}
	if claimed, err := repo.CreateBootstrapAdmin(context.Background(), user2); err != nil || claimed {
		t.Fatalf("second claim: claimed=%v err=%v", claimed, err)
	}
	var users int64
	if err := db.Model(&models.User{}).Count(&users).Error; err != nil || users != 1 {
		t.Fatalf("user rows=%d err=%v（让位分支不得写入用户）", users, err)
	}
}

// 占位已被消费（消费行先在、用户表为空）：claimed=false，注册仍可正常落库。
func TestCreateBootstrapAdminPreclaimedYields(t *testing.T) {
	db := bootstrapTestDB(t)
	repo := NewGormRepository(db)
	if err := db.Create(&models.AuthBootstrapClaim{ID: authBootstrapClaimID}).Error; err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	user := &models.User{Username: "root", Email: "root@x.com", Password: "x", Status: "active"}
	claimed, err := repo.CreateBootstrapAdmin(context.Background(), user)
	if err != nil || claimed {
		t.Fatalf("pre-claimed: claimed=%v err=%v", claimed, err)
	}
	var claims, users int64
	_ = db.Model(&models.AuthBootstrapClaim{}).Count(&claims).Error
	_ = db.Model(&models.User{}).Count(&users).Error
	if claims != 1 || users != 0 {
		t.Fatalf("claims=%d users=%d（消费分支不得追加占位或写入用户）", claims, users)
	}
}

// 插入失败：整事务回滚，占位不残留（重试仍可引导）。
func TestCreateBootstrapAdminInsertFailureReleasesClaim(t *testing.T) {
	db := bootstrapTestDB(t)
	repo := NewGormRepository(db)
	authExecTrigger(t, db, "CREATE TRIGGER blk_bootstrap_users BEFORE INSERT ON users BEGIN SELECT RAISE(ABORT, 'unique violation simulation'); END;")

	user := &models.User{Username: "root", Email: "root@x.com", Password: "x", Status: "active"}
	if claimed, err := repo.CreateBootstrapAdmin(context.Background(), user); err == nil || claimed {
		t.Fatalf("expected insert error, got claimed=%v err=%v", claimed, err)
	}
	var claims int64
	if err := db.Model(&models.AuthBootstrapClaim{}).Count(&claims).Error; err != nil || claims != 0 {
		t.Fatalf("claims=%d err=%v（失败必须回滚占位）", claims, err)
	}
}

// 计数失败 fail-closed（Critical-2）：第 2 次 SELECT（CountUsers）强制失败 →
// 注册以 ErrAuthRegisterUnavailable 拒绝，不落用户也不落占位。
func TestRegisterCountErrorFailsClosed(t *testing.T) {
	db := bootstrapTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), testAuthConfig())
	authFailNthQuery(db, 2)

	if _, err := svc.Register(context.Background(), bootstrapAdminInput(), slapp.AuthSessionMetadata{}); !errors.Is(err, slapp.ErrAuthRegisterUnavailable) {
		t.Fatalf("want ErrAuthRegisterUnavailable, got %v", err)
	}
	var users, claims int64
	_ = db.Model(&models.User{}).Count(&users).Error
	_ = db.Model(&models.AuthBootstrapClaim{}).Count(&claims).Error
	if users != 0 || claims != 0 {
		t.Fatalf("users=%d claims=%d（fail-closed 不得有副作用）", users, claims)
	}
}

// 让位回滚语义：服务层把 claimed=false 降级为 customer 正常落库（全链路）。
func TestRegisterBootstrapYieldDowngradesServiceLevel(t *testing.T) {
	db := bootstrapTestDB(t)
	svc := slapp.NewService(NewGormRepository(db), testAuthConfig())

	// 先消费占位（模拟另一请求已引导），用户表仍为空
	if err := db.Create(&models.AuthBootstrapClaim{ID: authBootstrapClaimID}).Error; err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	result, err := svc.Register(context.Background(), bootstrapAdminInput(), slapp.AuthSessionMetadata{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if result.User.Role != "customer" {
		t.Fatalf("role=%q want customer（占位被消费即让位降级）", result.User.Role)
	}
}
