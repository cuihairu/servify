package infra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	customerapp "servify/apps/server/internal/modules/customer/application"
	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/usersecurity"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateCustomer(ctx context.Context, cmd customerapp.CreateCustomerCommand) (*models.User, error) {
	var existing models.User
	if err := r.db.WithContext(ctx).Where("username = ? OR email = ?", cmd.Username, cmd.Email).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("username or email already exists")
	}

	user := &models.User{
		Username: cmd.Username,
		Email:    cmd.Email,
		Name:     cmd.Name,
		Phone:    cmd.Phone,
		Role:     "customer",
		Status:   "active",
	}
	customer := &models.Customer{
		Company:  cmd.Company,
		Industry: cmd.Industry,
		Source:   cmd.Source,
		Tags:     strings.Join(cmd.Tags, ","),
		Notes:    cmd.Notes,
		Priority: cmd.Priority,
	}
	applyCustomerScopeFields(ctx, customer)
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		customer.UserID = user.ID
		if err := tx.Create(customer).Error; err != nil {
			return fmt.Errorf("failed to create customer info: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return r.GetCustomerByID(ctx, user.ID)
}

func (r *GormRepository) GetCustomerByID(ctx context.Context, customerID uint) (*models.User, error) {
	var user models.User
	query := r.db.WithContext(ctx).Model(&models.User{}).
		Joins("JOIN customers ON customers.user_id = users.id")
	query = applyCustomerScope(query, ctx)
	if err := query.Preload("Sessions", func(db *gorm.DB) *gorm.DB {
		db = db.Order("created_at DESC").Limit(10)
		if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
			db = db.Where("tenant_id = ?", tenantID)
		}
		if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
			db = db.Where("workspace_id = ?", workspaceID)
		}
		return db
	}).Preload("Tickets", func(db *gorm.DB) *gorm.DB {
		db = db.Order("created_at DESC").Limit(10)
		if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
			db = db.Where("tenant_id = ?", tenantID)
		}
		if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
			db = db.Where("workspace_id = ?", workspaceID)
		}
		return db
	}).First(&user, "users.id = ?", customerID).Error; err != nil {
		return nil, fmt.Errorf("customer not found: %w", err)
	}
	return &user, nil
}

func (r *GormRepository) UpdateCustomer(ctx context.Context, customerID uint, cmd customerapp.UpdateCustomerCommand) (*models.User, error) {
	userUpdates := make(map[string]interface{})
	if cmd.Name != nil {
		userUpdates["name"] = *cmd.Name
	}
	if cmd.Phone != nil {
		userUpdates["phone"] = *cmd.Phone
	}
	if cmd.Status != nil {
		userUpdates["status"] = *cmd.Status
	}
	customerUpdates := make(map[string]interface{})
	if cmd.Company != nil {
		customerUpdates["company"] = *cmd.Company
	}
	if cmd.Industry != nil {
		customerUpdates["industry"] = *cmd.Industry
	}
	if cmd.Source != nil {
		customerUpdates["source"] = *cmd.Source
	}
	if cmd.Tags != nil {
		customerUpdates["tags"] = strings.Join(*cmd.Tags, ",")
	}
	if cmd.Notes != nil {
		customerUpdates["notes"] = *cmd.Notes
	}
	if cmd.Priority != nil {
		customerUpdates["priority"] = *cmd.Priority
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(userUpdates) > 0 {
			result := tx.Model(&models.User{}).Where("id = ?", customerID).Updates(userUpdates)
			if result.Error != nil {
				return fmt.Errorf("failed to update user: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("customer not found: %w", gorm.ErrRecordNotFound)
			}
		}
		if len(customerUpdates) > 0 {
			result := applyCustomerScope(tx.Model(&models.Customer{}), ctx).Where("user_id = ?", customerID).Updates(customerUpdates)
			if result.Error != nil {
				return fmt.Errorf("failed to update customer: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("customer not found: %w", gorm.ErrRecordNotFound)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return r.GetCustomerByID(ctx, customerID)
}

func (r *GormRepository) ListCustomers(ctx context.Context, query customerapp.ListCustomersQuery) ([]customerapp.CustomerInfoDTO, int64, error) {
	db := r.db.WithContext(ctx).Table("users").
		Select("users.*, customers.company, customers.industry, customers.source, customers.tags, customers.notes, customers.priority").
		Joins("LEFT JOIN customers ON users.id = customers.user_id").
		Where("users.role = ?", "customer")
	db = applyCustomerScope(db, ctx)

	if len(query.Industry) > 0 {
		db = db.Where("customers.industry IN ?", query.Industry)
	}
	if len(query.Source) > 0 {
		db = db.Where("customers.source IN ?", query.Source)
	}
	if len(query.Priority) > 0 {
		db = db.Where("customers.priority IN ?", query.Priority)
	}
	if len(query.Status) > 0 {
		db = db.Where("users.status IN ?", query.Status)
	}
	for _, tag := range query.Tags {
		db = db.Where("LOWER(customers.tags) LIKE LOWER(?)", "%"+tag+"%")
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		db = db.Where("LOWER(users.name) LIKE LOWER(?) OR LOWER(users.email) LIKE LOWER(?) OR LOWER(users.username) LIKE LOWER(?) OR LOWER(customers.company) LIKE LOWER(?)", search, search, search, search)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count customers: %w", err)
	}

	orderBy := fmt.Sprintf("users.%s %s", query.SortBy, query.SortOrder)
	offset := (query.Page - 1) * query.PageSize
	db = db.Order(orderBy).Offset(offset).Limit(query.PageSize)

	var items []customerapp.CustomerInfoDTO
	if err := db.Scan(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list customers: %w", err)
	}
	return items, total, nil
}

func (r *GormRepository) GetCustomerActivity(ctx context.Context, customerID uint, limit int) (*customerapp.CustomerActivityDTO, error) {
	activity := &customerapp.CustomerActivityDTO{CustomerID: customerID}
	sessionQuery := r.db.WithContext(ctx).Where("user_id = ?", customerID).Order("created_at DESC").Limit(limit)
	ticketQuery := r.db.WithContext(ctx).Where("customer_id = ?", customerID).Order("created_at DESC").Limit(limit)
	messageQuery := r.db.WithContext(ctx).Joins("JOIN sessions ON messages.session_id = sessions.id").Where("sessions.user_id = ?", customerID).Order("messages.created_at DESC").Limit(limit)
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		sessionQuery = sessionQuery.Where("tenant_id = ?", tenantID)
		ticketQuery = ticketQuery.Where("tenant_id = ?", tenantID)
		messageQuery = messageQuery.Where("messages.tenant_id = ? AND sessions.tenant_id = ?", tenantID, tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		sessionQuery = sessionQuery.Where("workspace_id = ?", workspaceID)
		ticketQuery = ticketQuery.Where("workspace_id = ?", workspaceID)
		messageQuery = messageQuery.Where("messages.workspace_id = ? AND sessions.workspace_id = ?", workspaceID, workspaceID)
	}
	sessionQuery.Find(&activity.RecentSessions)
	ticketQuery.Find(&activity.RecentTickets)
	messageQuery.Find(&activity.RecentMessages)
	return activity, nil
}

func (r *GormRepository) AddNote(ctx context.Context, customerID uint, note customerapp.CustomerNoteDTO) error {
	var customer models.Customer
	if err := applyCustomerScope(r.db.WithContext(ctx), ctx).Where("user_id = ?", customerID).First(&customer).Error; err != nil {
		return fmt.Errorf("customer not found: %w", err)
	}
	line := fmt.Sprintf("[%s] 用户%d: %s", note.CreatedAt.Format("2006-01-02 15:04:05"), note.AuthorID, note.Content)
	next := line
	if customer.Notes != "" {
		next = customer.Notes + "\n" + line
	}
	result := applyCustomerScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Where("user_id = ?", customerID).Update("notes", next)
	if result.Error != nil {
		return fmt.Errorf("failed to add note: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("customer not found: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *GormRepository) UpdateTags(ctx context.Context, customerID uint, tags []string) error {
	result := applyCustomerScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Where("user_id = ?", customerID).Update("tags", strings.Join(tags, ","))
	if result.Error != nil {
		return fmt.Errorf("failed to update tags: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("customer not found: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *GormRepository) GetStats(ctx context.Context) (*customerapp.CustomerStatsDTO, error) {
	stats := &customerapp.CustomerStatsDTO{}
	r.db.WithContext(ctx).Model(&models.User{}).Joins("JOIN customers ON customers.user_id = users.id").Where("role = ?", "customer").Scopes(func(db *gorm.DB) *gorm.DB {
		return applyCustomerScope(db, ctx)
	}).Count(&stats.Total)
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	r.db.WithContext(ctx).Model(&models.User{}).Joins("JOIN customers ON customers.user_id = users.id").Where("role = ? AND last_login > ?", "customer", thirtyDaysAgo).Scopes(func(db *gorm.DB) *gorm.DB {
		return applyCustomerScope(db, ctx)
	}).Count(&stats.Active)
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	r.db.WithContext(ctx).Model(&models.User{}).Joins("JOIN customers ON customers.user_id = users.id").Where("role = ? AND users.created_at > ?", "customer", sevenDaysAgo).Scopes(func(db *gorm.DB) *gorm.DB {
		return applyCustomerScope(db, ctx)
	}).Count(&stats.NewThisWeek)
	applyCustomerScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Select("source, COUNT(*) as count").Group("source").Scan(&stats.BySource)
	applyCustomerScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Select("industry, COUNT(*) as count").Group("industry").Having("industry != ''").Scan(&stats.ByIndustry)
	applyCustomerScope(r.db.WithContext(ctx).Model(&models.Customer{}), ctx).Select("priority, COUNT(*) as count").Group("priority").Scan(&stats.ByPriority)
	return stats, nil
}

func (r *GormRepository) RevokeCustomerTokens(ctx context.Context, customerID uint, revokeAt time.Time) (int, error) {
	if revokeAt.IsZero() {
		revokeAt = time.Now().UTC()
	}

	var customer models.Customer
	if err := applyCustomerScope(r.db.WithContext(ctx), ctx).Where("user_id = ?", customerID).First(&customer).Error; err != nil {
		return 0, fmt.Errorf("customer not found: %w", err)
	}

	version, err := usersecurity.RevokeUserTokens(ctx, r.db, customerID, revokeAt)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke customer tokens: %w", err)
	}
	return version, nil
}

func applyCustomerScope(db *gorm.DB, ctx context.Context) *gorm.DB {
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		db = db.Where("customers.tenant_id = ?", tenantID)
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		db = db.Where("customers.workspace_id = ?", workspaceID)
	}
	return db
}

func applyCustomerScopeFields(ctx context.Context, customer *models.Customer) {
	if customer == nil {
		return
	}
	if tenantID := platformauth.TenantIDFromContext(ctx); tenantID != "" {
		customer.TenantID = tenantID
	}
	if workspaceID := platformauth.WorkspaceIDFromContext(ctx); workspaceID != "" {
		customer.WorkspaceID = workspaceID
	}
}
