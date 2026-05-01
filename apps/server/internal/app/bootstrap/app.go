package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	appserver "servify/apps/server/internal/app/server"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Worker is the minimal lifecycle contract for background jobs managed by App.
type Worker interface {
	Name() string
	Start() error
	Stop(context.Context) error
}

// HTTPRuntime is the minimal lifecycle and router contract for HTTP-facing runtimes.
type HTTPRuntime interface {
	Start() error
	Stop(context.Context) error
	Router() http.Handler
}

// App is the bootstrap root for shared server runtime wiring.
type App struct {
	Config           *config.Config
	Logger           *logrus.Logger
	DB               *gorm.DB
	Redis            *redis.Client
	Runtime          HTTPRuntime
	Router           http.Handler
	Server           *http.Server
	EventBus         eventbus.Bus
	EmbeddingProvider embedding.Provider
	Workers          []Worker
	ShutdownHooks    []func() error
}

// BuildApp creates the shared application runtime dependencies used by entrypoints.
func BuildApp(cfg *config.Config) (*App, error) {
	logger, err := InitLogging(cfg)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	redisClient, err := OpenRedis(cfg)
	if err != nil {
		return nil, err
	}
	bus, err := BuildEventBus(cfg, logger, redisClient)
	if err != nil {
		if redisClient != nil {
			_ = redisClient.Close()
		}
		return nil, err
	}

	// 初始化 Embedding Provider
	embeddingProvider, err := embedding.NewProvider(embedding.FactoryConfig{
		Provider: cfg.Embedding.Provider,
		OpenAI: embedding.OpenAIProviderConfig{
			APIKey:  cfg.Embedding.OpenAI.APIKey,
			BaseURL: cfg.Embedding.OpenAI.BaseURL,
			Model:   cfg.Embedding.OpenAI.Model,
		},
		TEI: embedding.TEIProviderConfig{
			BaseURL: cfg.Embedding.TEI.BaseURL,
			Model:   cfg.Embedding.TEI.Model,
		},
		Xinference: embedding.XinferenceProviderConfig{
			BaseURL:  cfg.Embedding.Xinference.BaseURL,
			ModelUID: cfg.Embedding.Xinference.ModelUID,
		},
	})
	if err != nil {
		if redisClient != nil {
			_ = redisClient.Close()
		}
		return nil, fmt.Errorf("create embedding provider: %w", err)
	}

	app := &App{
		Config:           cfg,
		Logger:           logger,
		Redis:            redisClient,
		EventBus:         bus,
		EmbeddingProvider: embeddingProvider,
		Workers:          make([]Worker, 0),
		ShutdownHooks:    make([]func() error, 0),
	}
	if redisClient != nil {
		app.AddShutdownHook(func() error {
			return redisClient.Close()
		})
	}
	return app, nil
}

// RegisterWorker appends a managed background worker.
func (a *App) RegisterWorker(w Worker) {
	if w == nil {
		return
	}
	a.Workers = append(a.Workers, w)
}

// AttachHTTPRuntime records the runtime and its router on the app.
func (a *App) AttachHTTPRuntime(rt HTTPRuntime) {
	if a == nil {
		return
	}
	a.Runtime = rt
	if rt != nil {
		a.Router = rt.Router()
	}
}

// StartRuntime starts the attached runtime when present.
func (a *App) StartRuntime() error {
	if a == nil || a.Runtime == nil {
		return nil
	}
	return a.Runtime.Start()
}

// StartWorkers starts all registered workers in order.
func (a *App) StartWorkers() error {
	for _, w := range a.Workers {
		if err := w.Start(); err != nil {
			return fmt.Errorf("start worker %s: %w", w.Name(), err)
		}
	}
	return nil
}

// StopRuntime stops the attached runtime when present.
func (a *App) StopRuntime(ctx context.Context) error {
	if a == nil || a.Runtime == nil {
		return nil
	}
	return a.Runtime.Stop(ctx)
}

// BuildServerRuntime constructs the default HTTP runtime and attaches it to the app.
func (a *App) BuildServerRuntime() (*appserver.Runtime, error) {
	if a == nil {
		return nil, errors.New("bootstrap app is nil")
	}
	rt, err := appserver.BuildRuntime(a.Config, a.Logger, a.DB, a.Redis, a.EventBus)
	if err != nil {
		return nil, err
	}
	a.AttachHTTPRuntime(rt)
	return rt, nil
}

// AddShutdownHook appends a shutdown hook executed during app termination.
func (a *App) AddShutdownHook(hook func() error) {
	if hook == nil {
		return
	}
	a.ShutdownHooks = append(a.ShutdownHooks, hook)
}

// RunShutdownHooks runs hooks in reverse order.
func (a *App) RunShutdownHooks() error {
	for i := len(a.ShutdownHooks) - 1; i >= 0; i-- {
		if err := a.ShutdownHooks[i](); err != nil {
			return err
		}
	}
	return nil
}

// StopWorkers stops all registered workers in reverse order.
func (a *App) StopWorkers(ctx context.Context) error {
	for i := len(a.Workers) - 1; i >= 0; i-- {
		if err := a.Workers[i].Stop(ctx); err != nil {
			return fmt.Errorf("stop worker %s: %w", a.Workers[i].Name(), err)
		}
	}
	return nil
}

// Shutdown stops workers, runtime, and shutdown hooks in sequence and joins all errors.
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	var errs []error
	if err := a.StopWorkers(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := a.StopRuntime(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := a.RunShutdownHooks(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
