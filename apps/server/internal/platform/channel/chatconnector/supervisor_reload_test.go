package chatconnector

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/platform/channel"
)

// stateRecorder captures every Snapshot() transition for one installation
// key, so tests can assert the lifecycle without racing the goroutines.
func runnerStates(t *testing.T, sup *Supervisor) map[string]RunnerState {
	t.Helper()
	snapshot := sup.Snapshot()
	states := make(map[string]RunnerState, len(snapshot))
	for _, st := range snapshot {
		states[st.Platform+"/"+st.InstallationID] = st
	}
	return states
}

func TestSupervisorSnapshotTracksLifecycle(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{
		sampleRow("healthy"), sampleRow("doomed"),
	}}
	started := make(chan string, 2)
	factory := func(ctx context.Context, inst channel.ChatConnectorInstallation) (Runner, error) {
		if inst.InstallationID == "doomed" {
			return &scriptedRunner{started: started, id: inst.InstallationID,
				run: func(ctx context.Context) error {
					return errors.New("memex ingestion dead-lettered (attempts) after 10 attempts")
				},
			}, nil
		}
		return &scriptedRunner{started: started, id: inst.InstallationID}, nil
	}
	sup := NewSupervisor(lister, factory, nil)

	if got := sup.Snapshot(); len(got) != 0 {
		t.Fatalf("snapshot before start = %v, want empty", got)
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	collectStarted(t, started, 2)

	// while attached: both rows report a state with their scope intact. The
	// doomed runner may exit before the first observation, so only the
	// healthy one is guaranteed to still read running.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(runnerStates(t, sup)) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	states := runnerStates(t, sup)
	if len(states) != 2 {
		t.Fatalf("states = %d, want 2", len(states))
	}
	for key, st := range states {
		if st.Status != RunnerStatusRunning && !(key == "memex/doomed" && st.Status == RunnerStatusExited) {
			t.Fatalf("%s status = %q, want running (doomed may have exited already)", key, st.Status)
		}
		if st.TenantID != "tenant-1" || st.WorkspaceID != "workspace-1" || st.Platform != "memex" {
			t.Fatalf("%s scope drifted: %+v", key, st)
		}
		if st.Status == RunnerStatusRunning && st.Error != "" {
			t.Fatalf("%s running row must carry no error: %q", key, st.Error)
		}
	}

	// the doomed runner exits on its own: its state flips to exited with the
	// (content-free) terminal error, the healthy one keeps running
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if st := runnerStates(t, sup)["memex/doomed"]; st.Status == RunnerStatusExited {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	doomed := runnerStates(t, sup)["memex/doomed"]
	if doomed.Status != RunnerStatusExited || doomed.Error == "" || doomed.Since.IsZero() {
		t.Fatalf("doomed state = %+v, want exited with error and timestamp", doomed)
	}
	if got := runnerStates(t, sup)["memex/healthy"]; got.Status != RunnerStatusRunning {
		t.Fatalf("healthy state = %+v, want still running", got)
	}

	// Stop flips the runners it cancels to stopped; an already-exited runner
	// keeps its exited state (that is the dead-letter visibility)
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	states = runnerStates(t, sup)
	if len(states) != 2 {
		t.Fatalf("states after stop = %d, want 2 (kept for the ops surface)", len(states))
	}
	if st := states["memex/healthy"]; st.Status != RunnerStatusStopped {
		t.Fatalf("healthy status after stop = %q, want stopped", st.Status)
	}
	if st := states["memex/doomed"]; st.Status != RunnerStatusExited || st.Error == "" {
		t.Fatalf("doomed status after stop = %+v, want exited kept", st)
	}
}

func TestSupervisorReloadReattachesCurrentRows(t *testing.T) {
	lister := &fakeInstallationLister{rows: []channel.ChatConnectorInstallation{sampleRow("a")}}
	started := make(chan string, 2)
	sup := NewSupervisor(lister, quietFactory(started), nil)

	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := collectStarted(t, started, 1); got[0] != "a" {
		t.Fatalf("first start attached %v, want a", got)
	}

	// the enabled set changes (row toggled on while the service runs):
	// reload must re-list and attach exactly the current rows
	lister.rows = []channel.ChatConnectorInstallation{sampleRow("a"), sampleRow("b")}
	reloadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.Reload(reloadCtx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	// both fresh runners signal; "a" is a NEW runner after the reload, so the
	// channel sees a then b in whatever order the scheduler picks
	collectStarted(t, started, 2)
	states := runnerStates(t, sup)
	if len(states) != 2 || states["memex/a"].Status != RunnerStatusRunning ||
		states["memex/b"].Status != RunnerStatusRunning {
		t.Fatalf("states after reload = %+v, want a and b running", states)
	}

	if err := sup.Reload(reloadCtx); err != nil {
		t.Fatalf("second Reload: %v", err)
	}
	stopCtx, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSupervisorReloadWithoutStartIsNoop(t *testing.T) {
	sup := NewSupervisor(&fakeInstallationLister{}, quietFactory(make(chan string, 1)), nil)
	if err := sup.Reload(context.Background()); err != nil {
		t.Fatalf("Reload before Start: %v", err)
	}
}

func TestSupervisorReloadSurfacesStartFailure(t *testing.T) {
	lister := &fakeInstallationLister{err: errors.New("db down")}
	sup := NewSupervisor(lister, quietFactory(make(chan string, 1)), nil)

	// a list failure mid-reload must not leave the supervisor wedged: the
	// reload fails and a later reload on the same supervisor (store back)
	// works
	reloadCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sup.Reload(reloadCtx); err == nil {
		t.Fatal("reload with list error must fail")
	}
	lister.err = nil
	lister.rows = []channel.ChatConnectorInstallation{sampleRow("a")}
	started := make(chan string, 1)
	sup.factory = quietFactory(started)
	if err := sup.Reload(reloadCtx); err != nil {
		t.Fatalf("reload after recovery: %v", err)
	}
	collectStarted(t, started, 1)
	stopCtx, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := sup.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
