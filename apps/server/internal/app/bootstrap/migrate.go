package bootstrap

import (
	"servify/apps/server/internal/models"

	"gorm.io/gorm"
)

// MigrationModels returns the canonical model migration list.
func MigrationModels() []interface{} {
	return []interface{}{
		&models.User{},
		&models.UserAuthSession{},
		&models.RevokedToken{},
		&models.Customer{},
		&models.Agent{},
		&models.Session{},
		&models.Message{},
		&models.TransferRecord{},
		&models.WaitingRecord{},
		&models.Ticket{},
		&models.TicketComment{},
		&models.TicketFile{},
		&models.TicketStatus{},
		&models.CustomField{},
		&models.TicketCustomFieldValue{},
		&models.KnowledgeDoc{},
		&models.KnowledgeIndexJob{},
		&models.WebRTCConnection{},
		&models.DailyStats{},
		&models.SLAConfig{},
		&models.SLAViolation{},
		&models.CustomerSatisfaction{},
		&models.SatisfactionSurvey{},
		&models.AppIntegration{},
		&models.ShiftSchedule{},
		&models.AutomationTrigger{},
		&models.AutomationRun{},
		&models.Macro{},
		&models.TenantConfig{},
		&models.WorkspaceConfig{},
		&models.AuditLog{},
		&models.VoiceCall{},
		&models.VoiceRecording{},
		&models.VoiceTranscript{},
		&models.WebhookEndpoint{},
		&models.WebhookDelivery{},
		&models.APIKey{},
		&models.QualityReview{},
		&models.AgentGroup{},
		&models.AgentGroupMember{},
		&models.RemoteAssistSession{},
		&models.RemoteAssistAnnotation{},
		&models.AutomationTimer{},
		&models.UserRecoveryCode{},
	}
}

// AutoMigrate runs the canonical model set through GORM AutoMigrate.
//
// FROZEN: this is the legacy escape hatch (SERVIFY_AUTO_MIGRATE=on, sqlite
// databases and cmd/migrate -auto-migrate only). PostgreSQL databases are
// managed by versioned SQL migrations (see migrate_runner.go /
// migrations/000001_init.up.sql). Do not add new schema changes here — write
// the next numbered migration instead. The historical AddColumn patches below
// are already part of the baseline.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(MigrationModels()...)
}

// CreateIndexes applies additional runtime indexes used by the app.
func CreateIndexes(db *gorm.DB) error {
	statements := []string{
		"CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_tickets_status_created ON tickets(status, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_tickets_agent_status ON tickets(agent_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_tickets_customer_created ON tickets(customer_id, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_user_created ON sessions(user_id, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_user_auth_sessions_user_status ON user_auth_sessions(user_id, status, updated_at)",
		"CREATE INDEX IF NOT EXISTS idx_revoked_tokens_jti_revoked ON revoked_tokens(jti, revoked_at)",
		"CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at ON revoked_tokens(expires_at)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_agent_status ON sessions(agent_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_customers_priority ON customers(priority)",
		"CREATE INDEX IF NOT EXISTS idx_customers_source ON customers(source)",
		"CREATE INDEX IF NOT EXISTS idx_customers_industry ON customers(industry)",
		"CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status)",
		"CREATE INDEX IF NOT EXISTS idx_agents_department ON agents(department)",
		"CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON daily_stats(date)",
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
