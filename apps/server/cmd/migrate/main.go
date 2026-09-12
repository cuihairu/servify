package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	appbootstrap "servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// getenvDefault returns env var value or fallback if empty
func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func main() {
	cfg, err := appbootstrap.LoadConfig("")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI flags / env 覆盖
	var (
		flagConfig  string
		dbDriver    string
		flagDSN     string
		dbHost      string
		dbPortStr   string
		dbUser      string
		dbPass      string
		dbName      string
		dbSSLMode   string
		dbTZ        string
		withSeed    bool
		autoMigrate bool
	)

	defaultDSN := ""
	if strings.EqualFold(getenvDefault("DB_DRIVER", "postgres"), "sqlite") {
		defaultDSN = os.Getenv("DB_DSN")
	}

	flag.StringVar(&flagConfig, "config", "", "path to config file (default: ./config.yml)")
	flag.StringVar(&dbDriver, "db-driver", getenvDefault("DB_DRIVER", "postgres"), "database driver (postgres or sqlite)")
	flag.StringVar(&flagDSN, "dsn", defaultDSN, "database DSN; when db-driver=sqlite this should be a sqlite file path or DSN")
	flag.StringVar(&dbHost, "db-host", getenvDefault("DB_HOST", cfg.Database.Host), "database host")
	flag.StringVar(&dbPortStr, "db-port", getenvDefault("DB_PORT", fmt.Sprintf("%d", cfg.Database.Port)), "database port")
	flag.StringVar(&dbUser, "db-user", getenvDefault("DB_USER", cfg.Database.User), "database user")
	flag.StringVar(&dbPass, "db-pass", getenvDefault("DB_PASSWORD", cfg.Database.Password), "database password")
	flag.StringVar(&dbName, "db-name", getenvDefault("DB_NAME", cfg.Database.Name), "database name")
	flag.StringVar(&dbSSLMode, "db-sslmode", getenvDefault("DB_SSLMODE", "disable"), "sslmode (disable, require, verify-ca, verify-full)")
	flag.StringVar(&dbTZ, "db-timezone", getenvDefault("DB_TIMEZONE", "UTC"), "database timezone")
	flag.BoolVar(&withSeed, "seed", false, "seed default data after migration")
	flag.BoolVar(&autoMigrate, "auto-migrate", false, "use legacy GORM AutoMigrate instead of versioned SQL migrations (also implied for sqlite)")
	flag.Parse()

	// 如果指定了 --config，则重新加载配置文件
	if flagConfig != "" {
		cfg, err = appbootstrap.LoadConfig(flagConfig)
		if err != nil {
			log.Fatalf("Failed to load config %s: %v", flagConfig, err)
		}
	}

	// 解析端口
	if p, err := strconv.Atoi(dbPortStr); err == nil {
		// 同步到 cfg（方便后续日志打印），非必须
		cfg.Database.Port = p
	}

	// 组装 DSN（优先级：--dsn > 单项 DB flags/env > 配置文件）
	dsn := flagDSN
	if dsn == "" && dbDriver != "sqlite" {
		host := firstNonEmpty(dbHost, cfg.Database.Host)
		user := firstNonEmpty(dbUser, cfg.Database.User)
		pass := firstNonEmpty(dbPass, cfg.Database.Password)
		name := firstNonEmpty(dbName, cfg.Database.Name)
		port := dbPortStr
		if port == "" && cfg.Database.Port != 0 {
			port = fmt.Sprintf("%d", cfg.Database.Port)
		}
		ssl := dbSSLMode
		tz := dbTZ
		dsn = fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s", host, user, pass, name, port, ssl, tz)
	}

	// 连接数据库
	db, err := appbootstrap.OpenDatabase(cfg, appbootstrap.DatabaseOptions{
		Driver:   dbDriver,
		DSN:      dsn,
		LogLevel: logger.Info,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Starting database migration...")

	if appbootstrap.UsesVersionedMigrations(dbDriver) && !autoMigrate {
		if err := appbootstrap.RunMigrations(db); err != nil {
			log.Fatalf("Failed to run versioned migrations: %v", err)
		}
		log.Println("Versioned migrations applied successfully!")
	} else {
		if autoMigrate {
			log.Println("-auto-migrate set; running legacy GORM AutoMigrate")
		}
		if err := appbootstrap.AutoMigrate(db); err != nil {
			log.Fatalf("Failed to migrate database: %v", err)
		}
		log.Println("Database migration completed successfully!")

		log.Println("Creating additional indexes...")
		if err := appbootstrap.CreateIndexes(db); err != nil {
			log.Fatalf("Failed to create indexes: %v", err)
		}
		log.Println("Additional indexes created successfully!")
	}

	// 插入默认数据
	if withSeed {
		log.Println("Seeding default data...")
		seedDefaultData(db)
		log.Println("Default data seeded successfully!")
	}

	log.Println("Migration process completed!")
}

func seedDefaultData(db *gorm.DB) {
	// 创建默认管理员用户
	var adminUser models.User
	if err := db.Where("username = ?", "admin").First(&adminUser).Error; err != nil {
		adminUser = models.User{
			Username: "admin",
			Email:    "admin@servify.com",
			Name:     "系统管理员",
			Role:     "admin",
			Status:   "active",
		}
		db.Create(&adminUser)
		log.Println("Created default admin user")
	}

	// 创建测试客户
	var testCustomer models.User
	if err := db.Where("username = ?", "test_customer").First(&testCustomer).Error; err != nil {
		testCustomer = models.User{
			Username: "test_customer",
			Email:    "customer@test.com",
			Name:     "测试客户",
			Role:     "customer",
			Status:   "active",
		}
		db.Create(&testCustomer)

		// 创建客户扩展信息
		customer := models.Customer{
			UserID:   testCustomer.ID,
			Company:  "测试公司",
			Industry: "technology",
			Source:   "web",
			Priority: "normal",
			Tags:     "测试,新客户",
			Notes:    "这是一个测试客户账户",
		}
		db.Create(&customer)
		log.Println("Created test customer")
	}

	// 创建测试客服
	var testAgent models.User
	if err := db.Where("username = ?", "test_agent").First(&testAgent).Error; err != nil {
		testAgent = models.User{
			Username: "test_agent",
			Email:    "agent@test.com",
			Name:     "测试客服",
			Role:     "agent",
			Status:   "active",
		}
		db.Create(&testAgent)

		// 创建客服扩展信息
		agent := models.Agent{
			UserID:          testAgent.ID,
			Department:      "客户服务部",
			Skills:          "技术支持,产品咨询,投诉处理",
			Status:          "offline",
			MaxConcurrent:   5,
			CurrentLoad:     0,
			Rating:          5.0,
			AvgResponseTime: 30,
		}
		db.Create(&agent)
		log.Println("Created test agent")
	}

	// 创建示例知识库文档
	var existingDoc models.KnowledgeDoc
	if err := db.Where("title = ?", "欢迎使用 Servify").First(&existingDoc).Error; err != nil {
		doc := models.KnowledgeDoc{
			Title:    "欢迎使用 Servify",
			Content:  "Servify 是一个智能客服系统，集成了 AI 对话和人工客服功能。系统支持自动回复、工单管理、客户管理等功能。",
			Category: "getting-started",
			Tags:     "welcome,guide,introduction",
		}
		db.Create(&doc)
		log.Println("Created sample knowledge document")
	}

	// 创建示例统计数据
	var todayStats models.DailyStats
	today := time.Now().Truncate(24 * time.Hour)
	if err := db.Where("date = ?", today).First(&todayStats).Error; err != nil {
		stats := models.DailyStats{
			Date:                 today,
			TotalSessions:        10,
			TotalMessages:        50,
			TotalTickets:         5,
			ResolvedTickets:      3,
			AvgResponseTime:      45,
			AvgResolutionTime:    3600,
			CustomerSatisfaction: 4.2,
			AIUsageCount:         25,
			WeKnoraUsageCount:    15,
		}
		db.Create(&stats)
		log.Println("Created sample daily statistics")
	}
}
