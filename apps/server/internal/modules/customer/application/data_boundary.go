package application

import (
	"context"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
)

// PII 数据边界（V1.0 B2-2，docs/v1-convergence-plan.md §9.3-2 与 §5
// security 行）：本文件即 PII 清单的代码化——每类个人信息的落点表、
// 导出/擦除行为；管理面导出与删除（含关联擦除）读写口都从这份口径出发。
//
// 锚点约定：customers.UserID 是数据主体（users.id）；sessions.UserID 与
// tickets.CustomerID 都指向同一 user_id，messages 经 session_id 挂靠。

// PIIItem 清单条目。
type PIIItem struct {
	Category string   `json:"category"` // 类别
	Tables   []string `json:"tables"`   // 落点表
	Fields   []string `json:"fields"`   // 涉及字段
	Export   bool     `json:"export"`   // 数据主体导出是否包含
	Erasure  string   `json:"erasure"`  // anonymize / scrub / delete / never-exported
}

// ErasureErasedText 是 scrub 类字段的擦除占位文本。
const ErasureErasedText = "[已擦除]"

// PIICatalog PII 清单（代码即文档；新增 PII 落点必须同步登记）。
var PIICatalog = []PIIItem{
	{
		Category: "身份标识",
		Tables:   []string{"users"},
		Fields:   []string{"username", "email", "name", "phone", "avatar"},
		Export:   true,
		Erasure:  "anonymize",
	},
	{
		Category: "客户档案",
		Tables:   []string{"customers"},
		Fields:   []string{"company", "industry", "tags", "notes"},
		Export:   true,
		Erasure:  "anonymize",
	},
	{
		Category: "聊天记录",
		Tables:   []string{"messages"},
		Fields:   []string{"content"},
		Export:   true,
		Erasure:  "scrub",
	},
	{
		Category: "工单内容",
		Tables:   []string{"tickets", "ticket_comments"},
		Fields:   []string{"title", "description", "content"},
		Export:   true,
		Erasure:  "scrub",
	},
	{
		Category: "附件",
		Tables:   []string{"ticket_files"},
		Fields:   []string{"file_name", "file_path"},
		Export:   true,
		Erasure:  "delete",
	},
	{
		Category: "凭证与密钥",
		Tables:   []string{"users"},
		Fields:   []string{"password", "totp_secret"},
		Export:   false,
		Erasure:  "never-exported",
	},
}

// DataBoundaryRepository 数据边界读写口：跨表聚合导出与关联擦除。
// 这两个操作是横切数据主体的合规动作，直查跨模块表而不经各模块仓储
// （避免 customer 模块反向依赖 conversation/ticket 模块）。
type DataBoundaryRepository interface {
	// LoadCustomerWithUser 读客户档案与关联用户（任一不存在返回 ErrCustomerNotFound）。
	LoadCustomerWithUser(ctx context.Context, customerID uint) (*models.Customer, *models.User, error)
	// ListSessionsByUser 按数据主体 user_id 列会话。
	ListSessionsByUser(ctx context.Context, userID uint) ([]models.Session, error)
	// ListMessagesBySessions 按会话 ID 列消息（时序）。
	ListMessagesBySessions(ctx context.Context, sessionIDs []string) ([]models.Message, error)
	// ListTicketsByUser 按数据主体 user_id 列工单（含已删，导出需完整）。
	ListTicketsByUser(ctx context.Context, userID uint) ([]models.Ticket, error)
	// ListCommentsByTickets 列工单评论。
	ListCommentsByTickets(ctx context.Context, ticketIDs []uint) ([]models.TicketComment, error)
	// ListFilesByTickets 列工单附件元数据。
	ListFilesByTickets(ctx context.Context, ticketIDs []uint) ([]models.TicketFile, error)

	// EraseCustomerAndUser 匿名化 customers + users 两行（事务内由实现保证）。
	EraseCustomerAndUser(ctx context.Context, customerID, userID uint) error
	// ScrubMessagesBySessions 清空会话消息内容，返回影响行数。
	ScrubMessagesBySessions(ctx context.Context, sessionIDs []string) (int64, error)
	// ScrubTicketsByUser 清空工单标题/描述/AI 摘要（含已删行），返回影响行数。
	ScrubTicketsByUser(ctx context.Context, userID uint) (int64, error)
	// ScrubCommentsByTickets 清空工单评论内容（含已删行），返回影响行数。
	ScrubCommentsByTickets(ctx context.Context, ticketIDs []uint) (int64, error)
	// DeleteFilesByTickets 删除附件元数据（物理文件由对象存储清理流程处理），
	// 返回影响行数。
	DeleteFilesByTickets(ctx context.Context, ticketIDs []uint) (int64, error)
}

// DataBoundaryService 导出/擦除用例。
type DataBoundaryService struct {
	repo DataBoundaryRepository
	now  func() time.Time
}

// NewDataBoundaryService 构造；now 仅供测试注入。
func NewDataBoundaryService(repo DataBoundaryRepository) *DataBoundaryService {
	return &DataBoundaryService{repo: repo, now: time.Now}
}

// ErrCustomerNotFound 数据主体不存在。
var ErrCustomerNotFound = fmt.Errorf("customer not found")

// CustomerDataExport 数据主体导出载荷：覆盖 PIICatalog 中 Export=true
// 的全部类别；凭证类永不导出（结构体不含 password/totp_secret）。
type CustomerDataExport struct {
	ExportedAt time.Time              `json:"exported_at"`
	Catalog    []PIIItem              `json:"pii_catalog"`
	Customer   *models.Customer       `json:"customer"`
	Profile    *models.User           `json:"profile"`
	Sessions   []models.Session       `json:"sessions"`
	Messages   []models.Message       `json:"messages"`
	Tickets    []models.Ticket        `json:"tickets"`
	Comments   []models.TicketComment `json:"ticket_comments"`
	Files      []models.TicketFile    `json:"ticket_files"`
}

// ExportCustomerData 聚合导出数据主体全部 PII（管理面触发，audit 中间
// 件自动记录）。
func (s *DataBoundaryService) ExportCustomerData(ctx context.Context, customerID uint) (*CustomerDataExport, error) {
	if customerID == 0 {
		return nil, fmt.Errorf("customer_id required")
	}
	customer, user, err := s.repo.LoadCustomerWithUser(ctx, customerID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.repo.ListSessionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	sessionIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.ID)
	}
	messages, err := s.repo.ListMessagesBySessions(ctx, sessionIDs)
	if err != nil {
		return nil, err
	}
	tickets, err := s.repo.ListTicketsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	ticketIDs := make([]uint, 0, len(tickets))
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
	}
	comments, err := s.repo.ListCommentsByTickets(ctx, ticketIDs)
	if err != nil {
		return nil, err
	}
	files, err := s.repo.ListFilesByTickets(ctx, ticketIDs)
	if err != nil {
		return nil, err
	}
	return &CustomerDataExport{
		ExportedAt: s.now(),
		Catalog:    PIICatalog,
		Customer:   customer,
		Profile:    user,
		Sessions:   sessions,
		Messages:   messages,
		Tickets:    tickets,
		Comments:   comments,
		Files:      files,
	}, nil
}

// CustomerDataEraseResult 擦除结果摘要（管理面展示+审计）。
type CustomerDataEraseResult struct {
	CustomerID   uint      `json:"customer_id"`
	UserID       uint      `json:"user_id"`
	SessionsHit  int       `json:"sessions_hit"`
	MessagesHit  int64     `json:"messages_hit"`
	TicketsHit   int       `json:"tickets_hit"`
	CommentsHit  int64     `json:"comments_hit"`
	FilesDeleted int64     `json:"files_deleted"`
	ErasedAt     time.Time `json:"erased_at"`
}

// EraseCustomerData 删除数据主体 PII（含关联擦除，计划书 §9.3-2）：
// 身份/档案匿名化、聊天与工单内容 scrub、附件元数据删除；幂等（重复
// 擦除结果一致）。不硬删任何业务行——保留统计与审计的外键完整性。
func (s *DataBoundaryService) EraseCustomerData(ctx context.Context, customerID uint) (*CustomerDataEraseResult, error) {
	if customerID == 0 {
		return nil, fmt.Errorf("customer_id required")
	}
	_, user, err := s.repo.LoadCustomerWithUser(ctx, customerID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.repo.ListSessionsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	sessionIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.ID)
	}
	tickets, err := s.repo.ListTicketsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	ticketIDs := make([]uint, 0, len(tickets))
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
	}

	if err := s.repo.EraseCustomerAndUser(ctx, customerID, user.ID); err != nil {
		return nil, err
	}
	var result CustomerDataEraseResult
	if len(sessionIDs) > 0 {
		messagesHit, err := s.repo.ScrubMessagesBySessions(ctx, sessionIDs)
		if err != nil {
			return nil, err
		}
		result.MessagesHit = messagesHit
	}
	ticketsHit, err := s.repo.ScrubTicketsByUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	result.TicketsHit = int(ticketsHit)
	if len(ticketIDs) > 0 {
		commentsHit, err := s.repo.ScrubCommentsByTickets(ctx, ticketIDs)
		if err != nil {
			return nil, err
		}
		result.CommentsHit = commentsHit
		filesDeleted, err := s.repo.DeleteFilesByTickets(ctx, ticketIDs)
		if err != nil {
			return nil, err
		}
		result.FilesDeleted = filesDeleted
	}
	result.CustomerID = customerID
	result.UserID = user.ID
	result.SessionsHit = len(sessionIDs)
	result.ErasedAt = s.now()
	return &result, nil
}
