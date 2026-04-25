package bootstrap

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"servify/apps/server/internal/config"

	"github.com/spf13/viper"
)

type RuntimeOverrides struct {
	Database DatabaseOptions
	HTTP     HTTPServerOptions
}

// LoadConfig loads configuration from the default path or a specific config file.
func LoadConfig(configPath string) (*config.Config, error) {
	viper.Reset()

	// Expand environment variables ${VAR} in config file BEFORE Viper parses types
	// This ensures ${DIFY_ENABLED} becomes "true"/"false" string before bool conversion
	expandedPath := expandEnvVarsInConfig(configPath)
	if expandedPath != "" {
		defer os.Remove(expandedPath)
		configPath = expandedPath
	}

	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		// Default to /app/config.yml for Docker environment
		defaultConfigPaths := []string{
			".",
			"..",
			filepath.Join("..", ".."),
			"/app",
		}
		for _, p := range defaultConfigPaths {
			viper.AddConfigPath(p)
		}
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if configPath != "" || !errors.As(err, &notFound) {
			return nil, err
		}
	}
	cfg, result, err := config.LoadWithResult()
	if err != nil {
		return nil, err
	}
	applyConfigEnvOverrides(cfg)
	// Log security warnings for development environments
	if len(result.Warnings) > 0 && cfg.Server.Environment != "production" {
		LogSecurityWarnings(nil, cfg)
	}
	return cfg, nil
}

// ResolveRuntimeOverrides parses server startup flags and environment overrides.
func ResolveRuntimeOverrides(cfg *config.Config, args []string, output io.Writer) (RuntimeOverrides, error) {
	if cfg == nil {
		cfg = config.GetDefaultConfig()
	}

	var (
		dbDriver  string
		flagDSN   string
		dbHost    string
		dbPortStr string
		dbUser    string
		dbPass    string
		dbName    string
		dbSSLMode string
		dbTZ      string
		srvHost   string
		srvPort   int
	)

	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	if output != nil {
		flagSet.SetOutput(output)
	} else {
		flagSet.SetOutput(io.Discard)
	}
	flagSet.StringVar(&dbDriver, "db-driver", getenvDefault("DB_DRIVER", "postgres"), "database driver (postgres or sqlite)")
	defaultDSN := ""
	if strings.EqualFold(getenvDefault("DB_DRIVER", "postgres"), "sqlite") {
		defaultDSN = os.Getenv("DB_DSN")
	}
	flagSet.StringVar(&flagDSN, "dsn", defaultDSN, "database DSN; when db-driver=sqlite this should be a sqlite file path or DSN")
	flagSet.StringVar(&dbHost, "db-host", getenvDefault("DB_HOST", cfg.Database.Host), "database host")
	flagSet.StringVar(&dbPortStr, "db-port", getenvDefault("DB_PORT", fmt.Sprintf("%d", cfg.Database.Port)), "database port")
	flagSet.StringVar(&dbUser, "db-user", getenvDefault("DB_USER", cfg.Database.User), "database user")
	flagSet.StringVar(&dbPass, "db-pass", getenvDefault("DB_PASSWORD", cfg.Database.Password), "database password")
	flagSet.StringVar(&dbName, "db-name", getenvDefault("DB_NAME", cfg.Database.Name), "database name")
	flagSet.StringVar(&dbSSLMode, "db-sslmode", getenvDefault("DB_SSLMODE", "disable"), "sslmode (disable, require, verify-ca, verify-full)")
	flagSet.StringVar(&dbTZ, "db-timezone", getenvDefault("DB_TIMEZONE", "UTC"), "database timezone")
	flagSet.StringVar(&srvHost, "host", getenvDefault("SERVIFY_HOST", cfg.Server.Host), "server host (listen)")
	flagSet.IntVar(&srvPort, "port", serverPortFromEnv(cfg.Server.Port), "server port (listen)")
	if err := flagSet.Parse(args); err != nil {
		return RuntimeOverrides{}, err
	}

	dbOpts := DatabaseOptions{
		Driver:   dbDriver,
		DSN:      flagDSN,
		Host:     dbHost,
		Port:     dbPortStr,
		User:     dbUser,
		Password: dbPass,
		Name:     dbName,
		SSLMode:  dbSSLMode,
		TimeZone: dbTZ,
	}
	if dbOpts.DSN == "" && normalizedDatabaseDriver(dbOpts) != "sqlite" {
		dbOpts.DSN = BuildPostgresDSN(cfg, dbOpts)
	}

	return RuntimeOverrides{
		Database: dbOpts,
		HTTP: HTTPServerOptions{
			Host: srvHost,
			Port: srvPort,
		},
	}, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serverPortFromEnv(defaultPort int) int {
	if p := os.Getenv("SERVIFY_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	return defaultPort
}

// expandEnvVarsInConfig reads the config file, expands ${VAR} environment variables,
// and writes to a temporary file. Returns the temp file path or empty string on error.
// Must be called BEFORE viper.ReadInConfig() to ensure ${VAR} is expanded before type parsing.
func expandEnvVarsInConfig(configPath string) string {
	var configFiles []string

	// Determine which config file(s) to read
	if configPath != "" {
		configFiles = []string{configPath}
	} else {
		// Search default locations
		for _, searchDir := range []string{".", "..", "../..", "/app"} {
			potentialPath := filepath.Join(searchDir, "config.yml")
			if info, err := os.Stat(potentialPath); err == nil && !info.IsDir() {
				configFiles = []string{potentialPath}
				break
			}
		}
	}

	if len(configFiles) == 0 {
		return ""
	}

	// Read and expand the first found config file
	content, err := os.ReadFile(configFiles[0])
	if err != nil {
		return ""
	}

	// Expand ${VAR} placeholders using environment variables
	// Note: os.ExpandEnv handles ${VAR} and $VAR syntax
	expanded := os.ExpandEnv(string(content))

	// Create a temp file for the expanded content
	tmpFile, err := os.CreateTemp("", "servify-config-*.yml")
	if err != nil {
		return ""
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(expanded); err != nil {
		os.Remove(tmpFile.Name())
		return ""
	}

	return tmpFile.Name()
}

func applyConfigEnvOverrides(cfg *config.Config) {
	if cfg == nil {
		return
	}

	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.AI.OpenAI.APIKey = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.AI.OpenAI.BaseURL = v
	}
	if v := os.Getenv("OPENAI_MODEL"); v != "" {
		cfg.AI.OpenAI.Model = v
	}

	if v := os.Getenv("DIFY_ENABLED"); v != "" {
		cfg.Dify.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("DIFY_BASE_URL"); v != "" {
		cfg.Dify.BaseURL = v
	}
	if v := os.Getenv("DIFY_API_KEY"); v != "" {
		cfg.Dify.APIKey = v
	}
	if v := os.Getenv("DIFY_DATASET_ID"); v != "" {
		cfg.Dify.DatasetID = v
	}

	if v := os.Getenv("WEKNORA_ENABLED"); v != "" {
		cfg.WeKnora.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("WEKNORA_BASE_URL"); v != "" {
		cfg.WeKnora.BaseURL = v
	}
	if v := os.Getenv("WEKNORA_API_KEY"); v != "" {
		cfg.WeKnora.APIKey = v
	}
	if v := os.Getenv("WEKNORA_TENANT_ID"); v != "" {
		cfg.WeKnora.TenantID = v
	}
	if v := os.Getenv("WEKNORA_KB_ID"); v != "" {
		cfg.WeKnora.KnowledgeBaseID = v
	}
}
