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
	"sort"
	"sync"
	"time"

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

// Runner status values for RunnerState.
const (
	RunnerStatusRunning = "running" // attached and polling
	RunnerStatusStopped = "stopped" // clean exit via cancellation (shutdown, reload)
	RunnerStatusExited  = "exited"  // terminal error: dead-letter budget, permanent platform error, store failure, factory failure
)

// RunnerState is the content-free observability face of one attached runner:
// what it is attached to, whether it is running, and — once it exited on its
// own — the terminal error (DeadLetterError and the other terminal classes are
// content-free by design) and when that happened. States survive Stop so the
// ops surface can see what the last generation did.
type RunnerState struct {
	Platform       string    `json:"platform"`
	InstallationID string    `json:"installation_id"`
	TenantID       string    `json:"tenant_id"`
	WorkspaceID    string    `json:"workspace_id"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	Since          time.Time `json:"since"` // when the current status began
}

// Supervisor starts and stops one Runner per enabled installation.
type Supervisor struct {
	installations InstallationLister
	factory       RunnerFactory
	logger        *logrus.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   map[string]chan struct{}
	states map[string]*RunnerState
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
	s.states = make(map[string]*RunnerState, len(rows))

	for _, inst := range rows {
		key := inst.Platform + "/" + inst.InstallationID
		state := &RunnerState{
			Platform: inst.Platform, InstallationID: inst.InstallationID,
			TenantID: inst.TenantID, WorkspaceID: inst.WorkspaceID,
			Status: RunnerStatusRunning, Since: time.Now(),
		}
		s.states[key] = state
		runner, ferr := s.factory(ctx, inst)
		if ferr != nil {
			state.Status = RunnerStatusExited
			state.Error = ferr.Error()
			state.Since = time.Now()
			s.logger.WithError(ferr).WithFields(logrus.Fields{
				"platform":     inst.Platform,
				"installation": inst.InstallationID,
				"tenant_id":    inst.TenantID,
				"workspace_id": inst.WorkspaceID,
			}).Error("chat connector supervisor: build runner failed")
			continue
		}
		done := make(chan struct{})
		s.done[key] = done
		go s.runOne(loopCtx, inst, runner, done, state)
	}
	return nil
}

// runOne runs the loop, records the terminal state and logs it. A context
// cancellation is the normal shutdown path and stays quiet; every other
// terminal error (dead-letter budget exhausted, permanent platform error,
// store failure) is surfaced for alerting — the DeadLetterError shape itself
// is content-free.
func (s *Supervisor) runOne(ctx context.Context, inst channel.ChatConnectorInstallation, runner Runner, done chan struct{}, state *RunnerState) {
	defer close(done)
	err := runner.Run(ctx)

	s.mu.Lock()
	state.Since = time.Now()
	if err != nil && ctx.Err() == nil {
		// a terminal error that cancellation did not cause: dead-letter
		// budget, permanent platform error or a store failure
		state.Status = RunnerStatusExited
		state.Error = err.Error()
	} else {
		// cancellation (shutdown/reload) and a clean self-return (fail-closed
		// ingestion gate) both read as stopped, not running
		state.Status = RunnerStatusStopped
	}
	s.mu.Unlock()

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

// Snapshot returns the current per-runner states, sorted stably by scope so
// operators and tests read a deterministic list.
func (s *Supervisor) Snapshot() []RunnerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RunnerState, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.TenantID != b.TenantID {
			return a.TenantID < b.TenantID
		}
		if a.WorkspaceID != b.WorkspaceID {
			return a.WorkspaceID < b.WorkspaceID
		}
		if a.Platform != b.Platform {
			return a.Platform < b.Platform
		}
		return a.InstallationID < b.InstallationID
	})
	return out
}

// Reload stops every attached runner and re-attaches from the current enabled
// set: the dead-letter and row-toggle disposition tool (operators restart
// connectors without a process restart). The passed context bounds both the
// shutdown wait and the re-list.
func (s *Supervisor) Reload(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
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
