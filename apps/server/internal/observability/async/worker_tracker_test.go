package async

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/observability/metrics"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeWorker struct {
	name     string
	startErr error
	stopErr  error
	started  int
	stopped  int
}

func (w *fakeWorker) Name() string { return w.name }

func (w *fakeWorker) Start() error {
	w.started++
	return w.startErr
}

func (w *fakeWorker) Stop(_ context.Context) error {
	w.stopped++
	return w.stopErr
}

func histogramSampleCount(t *testing.T, reg *metrics.Registry, name string) uint64 {
	t.Helper()
	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			if len(mf.GetMetric()) == 0 {
				t.Fatalf("metric %s has no series", name)
			}
			return mf.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}

func TestNewWorkerMetrics_RegistersCollectors(t *testing.T) {
	reg := metrics.NewRegistry()
	wm := NewWorkerMetrics(reg)
	if wm == nil {
		t.Fatal("expected non-nil WorkerMetrics")
	}

	wm.jobsTotal.WithLabelValues("warmup", "success").Inc()
	wm.jobDuration.WithLabelValues("warmup").Observe(0.001)
	wm.activeJobs.WithLabelValues("warmup").Inc()

	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	found := map[string]bool{}
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}
	for _, name := range []string{"worker_jobs_total", "worker_job_duration_seconds", "worker_active_jobs"} {
		if !found[name] {
			t.Fatalf("expected metric %s to be registered", name)
		}
	}
}

func TestNewWorkerMetrics_DuplicateRegistrationPanics(t *testing.T) {
	reg := metrics.NewRegistry()
	NewWorkerMetrics(reg)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	NewWorkerMetrics(reg)
}

func TestObservableWorker_NameDelegates(t *testing.T) {
	fw := &fakeWorker{name: "emailer"}
	w := NewObservableWorker(fw, nil)
	if w.Name() != "emailer" {
		t.Fatalf("expected emailer, got %s", w.Name())
	}
}

func TestObservableWorker_StartStop_Success(t *testing.T) {
	reg := metrics.NewRegistry()
	wm := NewWorkerMetrics(reg)
	fw := &fakeWorker{name: "emailer"}
	w := NewObservableWorker(fw, wm)

	if err := w.Start(); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if fw.started != 1 {
		t.Fatalf("expected inner Start called once, got %d", fw.started)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("emailer")); v != 1 {
		t.Fatalf("expected active gauge 1 during run, got %v", v)
	}
	// worker_jobs_total 为 job 轮次口径（由 TrackJob 记），Start 不计数。
	if v := testutil.ToFloat64(wm.jobsTotal.WithLabelValues("emailer", "success")); v != 0 {
		t.Fatalf("expected success job counter 0 (Start no longer counts), got %v", v)
	}

	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
	if fw.stopped != 1 {
		t.Fatalf("expected inner Stop called once, got %d", fw.stopped)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("emailer")); v != 0 {
		t.Fatalf("expected active gauge 0 after stop, got %v", v)
	}
}

func TestObservableWorker_StartStop_Failure(t *testing.T) {
	reg := metrics.NewRegistry()
	wm := NewWorkerMetrics(reg)
	startErr := errors.New("start boom")
	stopErr := errors.New("stop boom")
	fw := &fakeWorker{name: "mailer", startErr: startErr, stopErr: stopErr}
	w := NewObservableWorker(fw, wm)

	if err := w.Start(); !errors.Is(err, startErr) {
		t.Fatalf("expected start error, got %v", err)
	}
	// Start 失败不记 jobs_total（job 轮次口径归 TrackJob），但 gauge 保持
	// 抬升直到 Stop（既有语义：Start 抬、Stop 落）。
	if v := testutil.ToFloat64(wm.jobsTotal.WithLabelValues("mailer", "failure")); v != 0 {
		t.Fatalf("expected failure job counter 0 (Start no longer counts), got %v", v)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("mailer")); v != 1 {
		t.Fatalf("expected active gauge 1 after failed start, got %v", v)
	}

	if err := w.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("expected stop error, got %v", err)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("mailer")); v != 0 {
		t.Fatalf("expected active gauge 0 after stop, got %v", v)
	}
}

func TestObservableWorker_NilMetrics(t *testing.T) {
	fw := &fakeWorker{name: "crawler"}
	w := NewObservableWorker(fw, nil)

	if err := w.Start(); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}
	if fw.started != 1 || fw.stopped != 1 {
		t.Fatalf("expected inner worker called, started=%d stopped=%d", fw.started, fw.stopped)
	}
}

func TestTrackJob_NilMetrics(t *testing.T) {
	called := false
	calledErr := false

	if err := TrackJob("job", nil, func() error { called = true; return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fn to be called")
	}

	wantErr := errors.New("job failed")
	if err := TrackJob("job", nil, func() error { calledErr = true; return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("expected job error, got %v", err)
	}
	if !calledErr {
		t.Fatal("expected fn to be called")
	}
}

func TestTrackJob_Success(t *testing.T) {
	reg := metrics.NewRegistry()
	wm := NewWorkerMetrics(reg)

	var activeDuringRun float64
	err := TrackJob("scraper", wm, func() error {
		activeDuringRun = testutil.ToFloat64(wm.activeJobs.WithLabelValues("scraper"))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if activeDuringRun != 1 {
		t.Fatalf("expected active gauge 1 during job, got %v", activeDuringRun)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("scraper")); v != 0 {
		t.Fatalf("expected active gauge 0 after job, got %v", v)
	}
	if v := testutil.ToFloat64(wm.jobsTotal.WithLabelValues("scraper", "success")); v != 1 {
		t.Fatalf("expected success counter 1, got %v", v)
	}
	if c := histogramSampleCount(t, reg, "worker_job_duration_seconds"); c != 1 {
		t.Fatalf("expected 1 duration observation, got %d", c)
	}
}

func TestTrackJob_Failure(t *testing.T) {
	reg := metrics.NewRegistry()
	wm := NewWorkerMetrics(reg)

	wantErr := errors.New("scrape failed")
	err := TrackJob("scraper", wm, func() error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected job error, got %v", err)
	}
	if v := testutil.ToFloat64(wm.activeJobs.WithLabelValues("scraper")); v != 0 {
		t.Fatalf("expected active gauge 0 after job, got %v", v)
	}
	if v := testutil.ToFloat64(wm.jobsTotal.WithLabelValues("scraper", "failure")); v != 1 {
		t.Fatalf("expected failure counter 1, got %v", v)
	}
	if c := histogramSampleCount(t, reg, "worker_job_duration_seconds"); c != 1 {
		t.Fatalf("expected 1 duration observation, got %d", c)
	}
}
