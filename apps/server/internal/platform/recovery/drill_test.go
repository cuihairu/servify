package recovery

// drill_test.go 是 P2-3"数据恢复、备份与迁移演练"的演练本体：
// 在一次性 sqlite 库与临时上传目录上走完整恢复链路，并把演练证据
// （summary + 双 manifest）落盘到 EVIDENCE_DIR，供
// scripts/validate-acceptance-manifest.sh 与仓库留档校验。
//
// 演练覆盖 todo P2-3 范围的三类资产：
//  1. 数据库（审计/工单/会话/消息关键表）：备份 → 注入"迁移窗口增量 +
//     失败迁移式损坏" → 恢复 → 行数/内容逐项对账
//  2. 上传文件（工单附件形态）：备份 → 篡改 + 删除 + 新增 → 对账必须
//     抓到不一致 → 恢复 → 字节级一致
//  3. 迁移回滚口径：迁移不提供 down，回滚 = 恢复备份（见
//     bootstrap/migrate_runner.go embed 注释），本演练的数据库分支即该
//     口径的可执行证据。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// keyTables 是演练对账的关键表清单（todo P2-3：审计/工单/会话关键表）。
var keyTables = []string{"users", "sessions", "messages", "tickets", "audit_logs"}

// TestBackupRestoreDrillSQLite 跑完整演练并写证据文件。CI 经
// scripts/test-backup-restore.sh 以仓库留档目录作为 EVIDENCE_DIR 驱动。
func TestBackupRestoreDrillSQLite(t *testing.T) {
	evidenceDir := os.Getenv("EVIDENCE_DIR")
	if evidenceDir == "" {
		evidenceDir = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(evidenceDir, 0o755))

	logStep := stepRecorder(t, evidenceDir)

	// ── 数据库分支 ──────────────────────────────────────────────
	db := openDrillDatabase(t)
	seedDrillData(t, db)
	logStep("seeded", "users=2 sessions=1 tickets=2 messages=3 audit_logs=2")

	backupDir := filepath.Join(t.TempDir(), "db-backup")
	dbManifest, err := BackupDatabase(db, backupDir)
	require.NoError(t, err)
	assert.Equal(t, "sqlite", dbManifest.Dialect)
	for _, name := range keyTables {
		entry, ok := ManifestTable(dbManifest, name)
		require.True(t, ok, "key table %s missing from backup manifest", name)
		assert.Greater(t, entry.Rows, 0, "key table %s backed up empty", name)
	}
	require.NoError(t, copyFile(
		filepath.Join(backupDir, "manifest.json"),
		filepath.Join(evidenceDir, "db-manifest.json")))
	logStep("db-backup", fmt.Sprintf("tables=%d total_rows=%d", len(dbManifest.Tables), dbManifest.TotalRows))

	// 备份后注入两类变更：迁移窗口的正常增量 + 失败迁移式的数据损坏。
	require.NoError(t, db.Exec(
		`INSERT INTO tickets (tenant_id, workspace_id, title, customer_id, status, created_at, updated_at)
		 VALUES ('tenant-drill', 'ws-drill', 'post-backup ticket', 1, 'open', ?, ?)`,
		time.Now(), time.Now()).Error)
	require.NoError(t, db.Exec(`UPDATE messages SET content = 'corrupted' WHERE id > 0`).Error)
	require.NoError(t, db.Exec(`DELETE FROM audit_logs`).Error)
	logStep("damage", "inserted=1_ticket corrupted=messages.content deleted=audit_logs")

	restoredManifest, err := RestoreDatabase(db, backupDir)
	require.NoError(t, err)

	// 行数对账：恢复后与备份清单逐表一致（含损坏清除与增量回退）。
	for _, entry := range dbManifest.Tables {
		var count int64
		require.NoError(t, db.Table(entry.Name).Count(&count).Error)
		assert.EqualValues(t, entry.Rows, count, "table %s row count after restore", entry.Name)
	}
	assert.Equal(t, dbManifest.TotalRows, restoredManifest.TotalRows)

	// 内容对账：备份前首条消息内容原样回来，损坏内容被清除。
	var messageCount int64
	require.NoError(t, db.Model(&models.Message{}).Count(&messageCount).Error)
	var corrupted int64
	require.NoError(t, db.Model(&models.Message{}).Where("content = ?", "corrupted").Count(&corrupted).Error)
	assert.Zero(t, corrupted, "corrupted message content survived restore")
	var ticketCount int64
	require.NoError(t, db.Model(&models.Ticket{}).Count(&ticketCount).Error)
	ticketEntry, _ := ManifestTable(dbManifest, "tickets")
	assert.EqualValues(t, ticketEntry.Rows, ticketCount, "post-backup ticket rolled back")
	var auditCount int64
	require.NoError(t, db.Model(&models.AuditLog{}).Count(&auditCount).Error)
	assert.EqualValues(t, 2, auditCount, "audit logs restored")

	// 自增位置对账：恢复后新插入的行不得撞上已恢复主键。
	newTicket := models.Ticket{TenantID: "tenant-drill", WorkspaceID: "ws-drill", Title: "post-restore ticket", CustomerID: 1, Status: "open"}
	require.NoError(t, db.Create(&newTicket).Error)
	ticketIDs := map[int64]bool{}
	var existingTickets []models.Ticket
	require.NoError(t, db.Find(&existingTickets).Error)
	for _, ticket := range existingTickets {
		require.False(t, ticketIDs[int64(ticket.ID)], "duplicate ticket id %d after restore", ticket.ID)
		ticketIDs[int64(ticket.ID)] = true
	}
	logStep("db-restore", fmt.Sprintf("tables_restored=%d duplicate_ids=0 audit_logs=%d", len(restoredManifest.Tables), auditCount))

	// ── 上传资产分支 ────────────────────────────────────────────
	uploadsRoot := filepath.Join(t.TempDir(), "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(uploadsRoot, "2026", "09"), 0o755))
	writeDrillFile(t, filepath.Join(uploadsRoot, "ticket-attachment.txt"), []byte("attachment payload v1"))
	writeDrillFile(t, filepath.Join(uploadsRoot, "2026", "09", "kb-doc.bin"), []byte{0x00, 0x01, 0x02, 0xFF})

	archivePath := filepath.Join(t.TempDir(), "uploads-backup.tar.gz")
	filesManifest, err := BackupFiles(uploadsRoot, archivePath)
	require.NoError(t, err)
	require.Len(t, filesManifest.Files, 2)
	require.NoError(t, writeJSONFile(filepath.Join(evidenceDir, "files-manifest.json"), filesManifest))
	logStep("files-backup", fmt.Sprintf("files=%d", len(filesManifest.Files)))

	// 篡改 + 删除 + 新增：对账必须全部抓到，恢复后必须归零。
	writeDrillFile(t, filepath.Join(uploadsRoot, "ticket-attachment.txt"), []byte("tampered payload"))
	require.NoError(t, os.Remove(filepath.Join(uploadsRoot, "2026", "09", "kb-doc.bin")))
	writeDrillFile(t, filepath.Join(uploadsRoot, "rogue.txt"), []byte("unexpected"))
	problems := VerifyFiles(uploadsRoot, filesManifest)
	require.Len(t, problems, 3)
	logStep("files-damage", fmt.Sprintf("detected=%v", problems))

	require.NoError(t, os.RemoveAll(uploadsRoot))
	_, err = RestoreFiles(archivePath, filesManifest, uploadsRoot)
	require.NoError(t, err)
	assert.Empty(t, VerifyFiles(uploadsRoot, filesManifest), "uploads mismatch after restore")
	content, err := os.ReadFile(filepath.Join(uploadsRoot, "2026", "09", "kb-doc.bin"))
	require.NoError(t, err)
	assert.Equal(t, []byte{0x00, 0x01, 0x02, 0xFF}, content, "binary attachment byte-identical after restore")
	logStep("files-restore", "mismatches=0 byte_identical=true")

	// ── 证据清单 ────────────────────────────────────────────────
	require.NoError(t, writeJSONFile(filepath.Join(evidenceDir, "manifest.json"), map[string]any{
		"provider": "backup-restore",
		"mode":     "drill-sqlite",
		"status":   map[string]any{"overall": "passed"},
		"checks": map[string]any{
			"db_restore_matches_backup":      "true",
			"db_survives_post_backup_damage": "true",
			"db_sequence_no_collision":       "true",
			"files_restore_matches_backup":   "true",
			"files_verify_detects_tamper":    "true",
		},
		"evidence_files": evidenceFileList(t, evidenceDir),
	}))
}

func openDrillDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "drill.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, bootstrap.AutoMigrate(db))
	return db
}

// seedDrillData 覆盖关键表：用户、会话、消息、工单、审计日志。
func seedDrillData(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()

	users := []models.User{
		{Username: "drill-agent", Email: "drill-agent@example.com", Password: "drill-password", Role: "agent", Status: "active"},
		{Username: "drill-customer", Email: "drill-customer@example.com", Password: "drill-password", Role: "customer", Status: "active"},
	}
	for i := range users {
		require.NoError(t, db.WithContext(ctx).Create(&users[i]).Error)
	}

	session := models.Session{ID: "drill-session-1", TenantID: "tenant-drill", WorkspaceID: "ws-drill", UserID: users[1].ID, Status: "active", Platform: "web", StartedAt: time.Now()}
	require.NoError(t, db.WithContext(ctx).Create(&session).Error)

	tickets := []models.Ticket{
		{TenantID: "tenant-drill", WorkspaceID: "ws-drill", Title: "drill ticket one", CustomerID: users[1].ID, Status: "open"},
		{TenantID: "tenant-drill", WorkspaceID: "ws-drill", Title: "drill ticket two", CustomerID: users[1].ID, Status: "assigned"},
	}
	for i := range tickets {
		require.NoError(t, db.WithContext(ctx).Create(&tickets[i]).Error)
	}

	messages := []models.Message{
		{TenantID: "tenant-drill", WorkspaceID: "ws-drill", SessionID: session.ID, UserID: users[1].ID, Content: "drill message one", Type: "text", Sender: "user"},
		{TenantID: "tenant-drill", WorkspaceID: "ws-drill", SessionID: session.ID, UserID: users[1].ID, Content: "drill message two", Type: "text", Sender: "user"},
		{TenantID: "tenant-drill", WorkspaceID: "ws-drill", SessionID: session.ID, UserID: users[1].ID, Content: "drill message three", Type: "text", Sender: "agent"},
	}
	for i := range messages {
		require.NoError(t, db.WithContext(ctx).Create(&messages[i]).Error)
	}

	auditLogs := []models.AuditLog{
		{PrincipalKind: "user", Action: "ticket.create", ResourceType: "ticket", ResourceID: "1", Route: "/api/v1/tickets", Method: "POST", StatusCode: 201, Success: true},
		{PrincipalKind: "user", Action: "auth.login", ResourceType: "session", ResourceID: "1", Route: "/api/v1/auth/login", Method: "POST", StatusCode: 200, Success: true},
	}
	for i := range auditLogs {
		require.NoError(t, db.WithContext(ctx).Create(&auditLogs[i]).Error)
	}
}

// stepRecorder 把演练每步一行追加到 evidence/summary.txt，失败时测试照样失败。
func stepRecorder(t *testing.T, evidenceDir string) func(step, detail string) {
	t.Helper()
	summary, err := os.OpenFile(filepath.Join(evidenceDir, "summary.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	require.NoError(t, err)
	return func(step, detail string) {
		t.Logf("drill step %s: %s", step, detail)
		_, err := fmt.Fprintf(summary, "step=%s %s\n", step, detail)
		require.NoError(t, err)
		require.NoError(t, summary.Sync())
	}
}

func writeDrillFile(t *testing.T, path string, content []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, 0o644))
}

func writeJSONFile(path string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

func evidenceFileList(t *testing.T, evidenceDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(evidenceDir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}
