package bootstrap

import (
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/config"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gormtracing "gorm.io/plugin/opentelemetry/tracing"
)

type DatabaseOptions struct {
	Driver        string
	DSN           string
	Host          string
	Port          string
	User          string
	Password      string
	Name          string
	SSLMode       string
	TimeZone      string
	LogLevel      logger.LogLevel
	EnableTracing bool
}

type DatabaseRetryOptions struct {
	MaxRetries int
	RetryDelay time.Duration
	Logger     *logrus.Logger
}

func normalizedDatabaseDriver(opts DatabaseOptions) string {
	driver := strings.TrimSpace(strings.ToLower(opts.Driver))
	switch driver {
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return "postgres"
	}
}

// BuildPostgresDSN composes a DSN from explicit options and config defaults.
func BuildPostgresDSN(cfg *config.Config, opts DatabaseOptions) string {
	if opts.DSN != "" {
		return opts.DSN
	}
	dbCfg := config.DatabaseConfig{}
	if cfg != nil {
		dbCfg = cfg.Database
	}
	host := firstNonEmpty(opts.Host, dbCfg.Host)
	user := firstNonEmpty(opts.User, dbCfg.User)
	pass := firstNonEmpty(opts.Password, dbCfg.Password)
	name := firstNonEmpty(opts.Name, dbCfg.Name)
	port := firstNonEmpty(opts.Port, fmt.Sprintf("%d", dbCfg.Port))
	sslMode := firstNonEmpty(opts.SSLMode, "disable")
	timeZone := firstNonEmpty(opts.TimeZone, "UTC")
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		host, user, pass, name, port, sslMode, timeZone,
	)
}

// OpenDatabase opens GORM and injects tracing when requested.
func OpenDatabase(cfg *config.Config, opts DatabaseOptions) (*gorm.DB, error) {
	level := opts.LogLevel
	if level == 0 {
		level = logger.Warn
	}
	driver := normalizedDatabaseDriver(opts)
	dsn := opts.DSN
	if dsn == "" {
		dsn = BuildPostgresDSN(cfg, opts)
	}

	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		dialector = postgres.Open(dsn)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                                   logger.Default.LogMode(level),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, err
	}
	applyConnectionPoolSettings(db, cfg)
	enableTracing := opts.EnableTracing
	if cfg != nil && cfg.Monitoring.Tracing.Enabled {
		enableTracing = true
	}
	if enableTracing {
		_ = db.Use(gormtracing.NewPlugin())
	}
	return db, nil
}

// applyConnectionPoolSettings wires the configured pool limits onto the
// underlying *sql.DB. These options were historically parsed from config but
// never applied; zero values keep database/sql defaults.
func applyConnectionPoolSettings(db *gorm.DB, cfg *config.Config) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	if cfg == nil {
		return
	}
	if cfg.Database.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	}
}

// OpenDatabaseWithRetry opens the database with bounded retry for container startup.
func OpenDatabaseWithRetry(cfg *config.Config, opts DatabaseOptions, retryOpts DatabaseRetryOptions) (*gorm.DB, error) {
	maxRetries := retryOpts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}
	retryDelay := retryOpts.RetryDelay
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	logger := retryOpts.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		db, err := OpenDatabase(cfg, opts)
		if err == nil {
			return db, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			logger.Warnf("Failed to connect to database (attempt %d/%d): %v, retrying in %v...", i+1, maxRetries, err, retryDelay)
			time.Sleep(retryDelay)
		}
	}
	return nil, fmt.Errorf("connect database after %d attempts: %w", maxRetries, lastErr)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
