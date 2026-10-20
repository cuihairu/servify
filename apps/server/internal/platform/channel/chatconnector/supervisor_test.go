package chatconnector

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/platform/channel"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

type fakeInstallationLister struct {
	rows []channel.ChatConnectorInstallation
	err  error
}

func (f *fakeInstallationLister) ListEnabled(ctx context.Context) ([]channel.ChatConnectorInstallation, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func sampleRow(installationID string) channel.ChatConnectorInstallation {
	return channel.ChatConnectorInstallation{
		Platform:       "memex",
		InstallationID: installationID,
		TenantID:       "tenant-1",
		WorkspaceID:    "workspace-1",
	}
}

// scriptedRunner signals that its loop started and then parks until the ctx is
// canceled (the quiet shutdown path). Tests override run for terminal-error or
// shutdown-deadline behavior.
type scriptedRunner struct {
	started chan<- string
	id      string
	run     func(ctx context.Context) error
}

func (r *scriptedRunner) Run(ctx context.Context) error {
	if r.started != nil {
		r.started <- r.id
	}
	if r.run != nil {
		return r.run(ctx)
	}
	<-ctx.Done()
	return ctx.Err()
}

func collectStarted(t *testing.T, started <-chan string, n int) []string {
	t.Helper()
	got := make([]string, 0, n)
	for len(got) < n {
		select {
		case id := <-started:
			got = append(got, id)
		case <-time.After(time.Second):
			t.Fatalf("started = %v, want %d runners", got, n)
		}
	}
	return got
}

func waitForEntry(t *testing.T, hook *test.Hook, message string) *logrus.Entry {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, e := range hook.AllEntries() {
			if e.Message == message {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func quietFactory(started chan<- string) RunnerFactory {
	return func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		return &scriptedRunner{started: started, id: inst.InstallationID}, nil
	}
}

func TestSupervisorStartsOneRunnerPerRow(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{
		sampleRow("a"), sampleRow("b"),
	}}
	started := make(chan string, 2)
	sup := NewSupervisor(lister, quietFactory(started), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := collectStarted(t, started, 2)
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen["a"] || !seen["b"] {
		t.Errorf("started = %v, want a and b", got)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorFactoryFailureSkipsOnlyThatRow(t *testing.T) {
	logger, hook := test.NewNullLogger()
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{
		sampleRow("broken"), sampleRow("healthy"),
	}}
	started := make(chan string, 1)
	var built []string
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		if inst.InstallationID == "broken" {
			return nil, errors.New("token env var not set")
		}
		built = append(built, inst.InstallationID)
		return &scriptedRunner{started: started, id: inst.InstallationID}, nil
	}
	sup := NewSupervisor(lister, factory, logger)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v (one bad row must not fail the rest)", err)
	}
	if got := collectStarted(t, started, 1); got[0] != "healthy" {
		t.Errorf("healthy row not started: %v", got)
	}
	if len(built) != 1 || built[0] != "healthy" {
		t.Errorf("built = %v, want only healthy", built)
	}
	entry := waitForEntry(t, hook, "chat connector supervisor: build runner failed")
	if entry == nil {
		t.Fatal("missing build-failure log entry")
	}
	if entry.Data["installation"] != "broken" {
		t.Errorf("build-failure entry installation = %v, want broken", entry.Data["installation"])
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorTerminalRunnerErrorIsLogged(t *testing.T) {
	logger, hook := test.NewNullLogger()
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 1)
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		return &scriptedRunner{started: started, id: inst.InstallationID,
			run: func(ctx context.Context) error { return errors.New("dead-letter budget exhausted") },
		}, nil
	}
	sup := NewSupervisor(lister, factory, logger)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	collectStarted(t, started, 1)
	entry := waitForEntry(t, hook, "chat connector runner terminated")
	if entry == nil {
		t.Fatal("missing terminal-error log entry")
	}
	if entry.Level != logrus.ErrorLevel {
		t.Errorf("terminal-error entry level = %v, want error", entry.Level)
	}
	if entry.Data["installation"] != "a" || entry.Data["tenant_id"] != "tenant-1" {
		t.Errorf("terminal-error entry fields = %v", entry.Data)
	}
}

func TestSupervisorStopCancelsRunnersQuietly(t *testing.T) {
	logger, hook := test.NewNullLogger()
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 1)
	sup := NewSupervisor(lister, quietFactory(started), logger)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	collectStarted(t, started, 1)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// cancellation is the normal shutdown path: no error-level entry
	time.Sleep(50 * time.Millisecond)
	for _, e := range hook.AllEntries() {
		if e.Level == logrus.ErrorLevel {
			t.Fatalf("quiet stop logged an error: %v", e)
		}
	}
}

func TestSupervisorStopIsBoundedByShutdownDeadline(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 1)
	release := make(chan struct{})
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		return &scriptedRunner{started: started, id: inst.InstallationID,
			// a wedged runner ignores cancellation entirely; the 2s backstop
			// just keeps a broken test from hanging forever
			run: func(ctx context.Context) error {
				select {
				case <-release:
				case <-time.After(2 * time.Second):
				}
				return nil
			},
		}, nil
	}
	sup := NewSupervisor(lister, factory, nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	collectStarted(t, started, 1)

	deadline, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := sup.Stop(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want DeadlineExceeded", err)
	}
	// a stuck runner does not wedge the supervisor forever: a second Stop is a
	// clean no-op, and the goroutine may still drain afterwards
	close(release)
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestSupervisorStartIsIdempotent(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 2)
	sup := NewSupervisor(lister, quietFactory(started), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	collectStarted(t, started, 1)
	select {
	case id := <-started:
		t.Fatalf("second Start attached a duplicate runner for %q", id)
	case <-time.After(100 * time.Millisecond):
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorStopWithoutStartReturnsNil(t *testing.T) {
	sup := NewSupervisor(&fakeInstallationLister{}, quietFactory(make(chan string, 1)), nil)
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}

func TestSupervisorWithoutStoreStartsNothing(t *testing.T) {
	logger, hook := test.NewNullLogger()
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		t.Fatal("factory must not run without an installation store")
		return nil, nil
	}
	sup := NewSupervisor(nil, factory, logger)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	entry := waitForEntry(t, hook, "chat connector supervisor: installation store not configured, no connector started")
	if entry == nil {
		t.Fatal("missing missing-store warning")
	}
	if err := sup.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorListErrorFailsStartThenFreshStartWorks(t *testing.T) {
	lister := &fakeInstallationLister{err: errors.New("db down")}
	var factoryCalls int
	started := make(chan string, 1)
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		factoryCalls++
		return &scriptedRunner{started: started, id: inst.InstallationID}, nil
	}
	sup := NewSupervisor(lister, factory, nil)

	if err := sup.Start(context.Background()); err == nil {
		t.Fatal("Start with list error must fail")
	}
	if factoryCalls != 0 {
		t.Fatal("factory must not run when listing fails")
	}

	// a failed start must leave the same supervisor restartable once the store
	// is reachable again
	lister.err = nil
	lister.rows = []channel.ChatConnectorInstallation{sampleRow("a")}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("restart after list error: %v", err)
	}
	collectStarted(t, started, 1)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorRestartAfterStopAttachesNewRows(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 2)
	sup := NewSupervisor(lister, quietFactory(started), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := collectStarted(t, started, 1); got[0] != "a" {
		t.Fatalf("first start attached %v, want a", got)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// the enabled set changed while stopped (row toggle or restart): the next
	// start attaches exactly the new set
	lister.rows = []channel.ChatConnectorInstallation{sampleRow("b")}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if got := collectStarted(t, started, 1); got[0] != "b" {
		t.Fatalf("restart attached %v, want b", got)
	}
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
