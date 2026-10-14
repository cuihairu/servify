package application

import (
	"context"

	"servify/apps/server/internal/models"
)

// Repository SLA 持久化端口（infra/gorm 实现）。
//
// 返回语义分三类，实现必须逐条遵守：
//   - 不存在即哨兵：GetConfigScoped / GetTicketScoped 映射 ErrRecordNotFound
//     为应用哨兵（404 语义，不泄漏存在性）。
//   - 不存在即 nil,nil：各 Find* 判重/回退查询以 (nil, nil) 表示「无匹配」，
//     仅把非 not-found 的存储错误上抛。
//   - 原样上抛：其余方法只返回裸错误。
type Repository interface {
	// ---- sla_configs ----
	CreateConfig(ctx context.Context, config *models.SLAConfig) error
	SaveConfig(ctx context.Context, config *models.SLAConfig) error
	GetConfigScoped(ctx context.Context, id uint) (*models.SLAConfig, error)
	DeleteConfig(ctx context.Context, id uint) (int64, error)
	// CountConfigsFiltered / ListConfigs 分两次查询（count 后 find），服务层对两类
	// 失败分别包装（"failed to count SLA configs" / "failed to list SLA configs"），
	// 顺序测试（failNthQuery）依赖此顺序。
	CountConfigsFiltered(ctx context.Context, req *SLAConfigListRequest) (int64, error)
	ListConfigs(ctx context.Context, req *SLAConfigListRequest) ([]models.SLAConfig, error)
	// FindConfigByPriorityTier 判重（创建路径）：无匹配 (nil, nil)。
	FindConfigByPriorityTier(ctx context.Context, priority, tier string) (*models.SLAConfig, error)
	// FindConfigByPriorityExceptID 判重（更新优先级路径，排除自身）：无匹配 (nil, nil)。
	FindConfigByPriorityExceptID(ctx context.Context, priority string, excludeID uint) (*models.SLAConfig, error)
	// FindConfigByPriorityTierExceptID 组合唯一性判重（更新路径，排除自身）：无匹配 (nil, nil)。
	FindConfigByPriorityTierExceptID(ctx context.Context, priority, tier string, excludeID uint) (*models.SLAConfig, error)
	// FindConfigByPriorityTierActive 优先匹配特定客户级别的活跃配置：无匹配 (nil, nil)。
	FindConfigByPriorityTierActive(ctx context.Context, priority, tier string) (*models.SLAConfig, error)
	// FindDefaultConfigByPriorityActive 回退默认配置（未指定客户级别）：无匹配 (nil, nil)。
	FindDefaultConfigByPriorityActive(ctx context.Context, priority string) (*models.SLAConfig, error)
	CountConfigs(ctx context.Context) (int64, error)
	CountActiveConfigs(ctx context.Context) (int64, error)

	// ---- tickets ----
	GetTicketScoped(ctx context.Context, id uint) (*models.Ticket, error)
	CountTickets(ctx context.Context) (int64, error)
	// CountTicketsByDate 按 created_at 日期的工单计数（趋势数据用）。
	CountTicketsByDate(ctx context.Context, date string) (int64, error)
	// ListActiveTickets 未关闭工单（监控扫描用）。
	ListActiveTickets(ctx context.Context) ([]models.Ticket, error)

	// ---- customers ----
	// GetCustomerPriorityScoped 按 user_id 读客户优先级当客户等级；调用方对任何
	// 错误都按「无等级」处理。
	GetCustomerPriorityScoped(ctx context.Context, userID uint) (string, error)

	// ---- sla_violations ----
	CreateViolation(ctx context.Context, violation *models.SLAViolation) error
	// GetViolationScoped 带关联预加载读回违约记录；任何失败原样上抛（不映射哨兵）。
	GetViolationScoped(ctx context.Context, id uint) (*models.SLAViolation, error)
	// FindViolationByTicketConfigType 判重同类型违约：无匹配 (nil, nil)。
	FindViolationByTicketConfigType(ctx context.Context, ticketID, configID uint, violationType string) (*models.SLAViolation, error)
	// CountViolationsFiltered / ListViolations 同上分两次查询（count 后 find）。
	CountViolationsFiltered(ctx context.Context, req *SLAViolationListRequest) (int64, error)
	ListViolations(ctx context.Context, req *SLAViolationListRequest) ([]models.SLAViolation, error)
	ResolveViolation(ctx context.Context, id uint) (int64, error)
	// ResolveViolationsByTicket 按工单批量置为已解决（可选限定类型）。
	ResolveViolationsByTicket(ctx context.Context, ticketID uint, violationTypes []string) (int64, error)
	CountViolations(ctx context.Context) (int64, error)
	CountViolationsByConfig(ctx context.Context, configID uint) (int64, error)
	CountUnresolvedViolations(ctx context.Context) (int64, error)
	// CountViolationsByDate 按 violated_at 日期的违约计数（趋势数据用）。
	CountViolationsByDate(ctx context.Context, date string) (int64, error)
	ViolationsByType(ctx context.Context) (map[string]int, error)
	ViolationsByPriority(ctx context.Context) (map[string]int, error)
	ViolationsByTier(ctx context.Context) (map[string]int, error)
}
