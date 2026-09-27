package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 链式防篡改（R2）：每条审计记录的 entry_hash = sha256(prev_hash ‖ 规范化字段)，
// prev_hash 取上一条（按 id 序）已哈希记录的 entry_hash，首条取 ChainGenesis。
// 任何对历史记录字段/哈希的改动都会让后续链路校验断裂；仓储写入在事务内
// 读链尾再落库（postgres 加行锁串行化，sqlite 单写者天然串行）。
//
// 语义边界（诚实声明）：
//   - 表头最旧记录被删（如保留期清理）时只能锚定校验（Anchored=true），
//     头部截断本身不可证伪——哈希链的固有信任根问题，报告给出 FirstID/Total
//     供运维与预期对账；
//   - 本特性上线前的存量记录（entry_hash 为空）视为 legacy 行：不参与哈希
//     校验，链在最后一个 legacy 行之后重新锚定，报告给出 legacy_rows 计数。

// ChainGenesis 空表首条记录的 prev_hash。
const ChainGenesis = "0000000000000000000000000000000000000000000000000000000000000000"

// chainFieldSep 规范化字段的分隔符（单元分隔符，避开 JSON/文本常见字符）。
const chainFieldSep = "\x1f"

// ChainReport 链式校验报告。
type ChainReport struct {
	OK         bool   `json:"ok"`
	Total      int64  `json:"total"`
	Hashed     int64  `json:"hashed"`
	LegacyRows int64  `json:"legacy_rows"`
	Anchored   bool   `json:"anchored"`
	FirstID    uint   `json:"first_id"`
	LastID     uint   `json:"last_id"`
	BrokenAtID uint   `json:"broken_at_id"`
	Reason     string `json:"reason,omitempty"`
}

// ChainVerifier 链式校验能力（GormQueryService 实现；测试桩可不实现）。
type ChainVerifier interface {
	VerifyChain(ctx context.Context) (*ChainReport, error)
}

// ComputeEntryHash 计算单条记录的链式哈希：sha256(prev ‖ 规范化字段)。
// 时间戳统一截断到微秒（postgres timestamptz 精度）保证写入/读回重算一致。
func ComputeEntryHash(prevHash string, row *models.AuditLog) string {
	if row == nil {
		return ""
	}
	actor := ""
	if row.ActorUserID != nil {
		actor = strconv.FormatUint(uint64(*row.ActorUserID), 10)
	}
	parts := []string{
		prevHash,
		actor,
		row.PrincipalKind,
		row.Action,
		row.ResourceType,
		row.ResourceID,
		row.Route,
		row.Method,
		strconv.Itoa(row.StatusCode),
		strconv.FormatBool(row.Success),
		row.RequestID,
		row.ClientIP,
		row.UserAgent,
		row.TenantID,
		row.WorkspaceID,
		row.RequestJSON,
		row.BeforeJSON,
		row.AfterJSON,
		row.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, chainFieldSep)))
	return hex.EncodeToString(sum[:])
}

// VerifyChain 按 id 升序走链校验全部审计记录。任何一行 prev/entry 哈希与
// 前驱脱节即报告断点（含中间行删除——被删行的哈希仍是后行 prev_hash 的
// 期望值，重算必然失配）。
func VerifyChain(ctx context.Context, db *gorm.DB) (*ChainReport, error) {
	if db == nil {
		return nil, errors.New("audit chain verify: nil db")
	}

	report := &ChainReport{OK: true}
	var rows []models.AuditLog
	if err := db.WithContext(ctx).Model(&models.AuditLog{}).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	report.Total = int64(len(rows))

	expected := ""
	for i := range rows {
		row := &rows[i]
		report.LastID = row.ID
		if row.EntryHash == "" {
			report.LegacyRows++
			expected = ""
			continue
		}
		if expected == "" {
			// 链起点：锚定在本行自带的 prev_hash（genesis 或被截断的头部）。
			report.FirstID = row.ID
			expected = row.PrevHash
		}
		if row.PrevHash != expected {
			report.OK = false
			report.BrokenAtID = row.ID
			report.Reason = "prev_hash_mismatch"
			return report, nil
		}
		if row.EntryHash != ComputeEntryHash(expected, row) {
			report.OK = false
			report.BrokenAtID = row.ID
			report.Reason = "entry_hash_mismatch"
			return report, nil
		}
		expected = row.EntryHash
		report.Hashed++
	}

	// 全链强度：首行即哈希行、从 genesis 起链、无 legacy 行——此时头部
	// 截断/清空都可证伪；否则只是锚定校验。
	if report.Total > 0 && report.LegacyRows == 0 && report.FirstID == rows[0].ID && rows[0].PrevHash == ChainGenesis {
		report.Anchored = false
	} else if report.Total > 0 {
		report.Anchored = true
	}
	return report, nil
}

// chainNeedsRowLock postgres/mysql 用行锁把"读链尾→写新行"串行化；
// sqlite 无 FOR UPDATE 语法，依赖单写者语义。
func chainNeedsRowLock(dialect string) bool {
	return dialect == "postgres" || dialect == "mysql"
}

// chainLockClauses postgres/mysql 读链尾加行锁串行化；sqlite 空子句集。
func chainLockClauses(lock bool) []clause.Expression {
	if lock {
		return []clause.Expression{clause.Locking{Strength: "UPDATE"}}
	}
	return nil
}

// chainTail 事务内读当前链尾哈希（最后一个已哈希行；legacy 行不接力）。
func chainTail(tx *gorm.DB) (string, error) {
	q := tx.Model(&models.AuditLog{}).
		Where("entry_hash <> ''").
		Order("id DESC").Limit(1).
		Clauses(chainLockClauses(chainNeedsRowLock(tx.Dialector.Name()))...)
	var hashes []string
	if err := q.Pluck("entry_hash", &hashes).Error; err != nil {
		return "", err
	}
	if len(hashes) == 0 {
		return ChainGenesis, nil
	}
	return hashes[0], nil
}
