// Package chatconnector owns the service-lifecycle supervision of chat
// platform connector installations: at startup it attaches one poll-loop
// Runner to every enabled installation row, and on shutdown it stops them.
//
// The installation-to-Runner construction itself stays outside this package
// (the assembly layer builds each memex.Runner with its token-scoped client
// and stores): the supervisor only owns lifecycle, so a Runner never has to
// expose its internals upward and a Runner that cannot be built fails only its
// own installation's attachment rather than the whole process startup.
package chatconnector

import (
	"context"
	"sync"

	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
)

// Runner is the lifecycle face one managed connector poll loop exposes —
// satisfied by *memex.Runner. Run blocks until ctx is canceled or the loop
// returns on its own (dead-letter, permanent error, fail-closed gate).
type Runner interface {
	Run(ctx context.Context) error
}

// RunnerFactory builds the Runner for one enabled installation row. Returning
// an error fails only that installation: the supervisor logs it and attaches
// the rest. The factory must inject the row's credential (env-var lookup),
// stores and endpoint — the row carries no credential itself.
type RunnerFactory func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error)

// InstallationLister reads the enabled installation set at startup. It is
// satisfied by *channel.InstallationStore.
type InstallationLister interface {
	ListEnabled(ctx context.Context) ([]channel.ChatConnectorInstallation, error)
}

// Supervisor starts and stops one Runner per enabled installation.
type Supervisor struct {
	installations InstallationLister
	factory       RunnerFactory
	logger        *logrus.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   map[string]chan struct{}
}

// NewSupervisor wires the supervisor to the installation store and the
// assembly-layer factory.
func NewSupervisor(installations InstallationLister, factory RunnerFactory, logger *logrus.Logger) *Supervisor {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Supervisor{
		installations: installations,
		factory:       factory,
		logger:        logger,
	}
}

// Start lists enabled installations and launches one goroutine per row. A
// missing store, a list failure, or a row whose factory returns an error are
// logged and reported: a connector wiring problem must not silently look
// healthy, but it also does not take the other channels down with it.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil // already running
	}
	if s.installations == nil {
		s.logger.Warn("chat connector supervisor: installation store not configured, no connector started")
		return nil
	}
	rows, err := s.installations.ListEnabled(ctx)
	if err != nil {
		return err
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(map[string]chan struct{}, len(rows))

	for _, inst := range rows {
		runner, ferr := s.factory(ctx, inst)
		if ferr != nil {
			s.logger.WithError(ferr).WithFields(logrus.Fields{
				"platform":     inst.Platform,
				"installation": inst.InstallationID,
				"tenant_id":    inst.TenantID,
				"workspace_id": inst.WorkspaceID,
			}).Error("chat connector supervisor: build runner failed")
			continue
		}
		done := make(chan struct{})
		s.done[inst.Platform+"/"+inst.InstallationID] = done
		go s.runOne(loopCtx, inst, runner, done)
	}
	return nil
}

// runOne runs the loop and logs its terminal error. A context cancellation is
// the normal shutdown path and stays quiet; every other terminal error
// (dead-letter budget exhausted, permanent platform error, store failure) is
// surfaced for alerting — the DeadLetterError shape itself is content-free.
func (s *Supervisor) runOne(ctx context.Context, inst channel.ChatConnectorInstallation, runner Runner, done chan struct{}) {
	defer close(done)
	err := runner.Run(ctx)
	fields := logrus.Fields{
		"platform":     inst.Platform,
		"installation": inst.InstallationID,
		"tenant_id":    inst.TenantID,
		"workspace_id": inst.WorkspaceID,
	}
	switch {
	case err == nil:
		s.logger.WithFields(fields).Info("chat connector runner stopped")
	case ctx.Err() != nil:
		// cancellation-driven: expected at shutdown
	default:
		s.logger.WithError(err).WithFields(fields).Error("chat connector runner terminated")
	}
}

// Stop cancels every loop and waits for all goroutines to finish, bounded by
// the passed context (the app shutdown deadline).
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	dones := s.done
	s.cancel = nil
	s.done = nil
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	for _, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
