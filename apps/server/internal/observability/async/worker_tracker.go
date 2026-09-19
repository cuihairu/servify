package async

import (
	"context"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	"servify/apps/server/internal/observability/metrics"
	"servify/apps/server/internal/observability/telemetry"

	"github.com/prometheus/client_golang/prometheus"
)

// WorkerMetrics holds Prometheus collectors for worker job observability.
type WorkerMetrics struct {
	jobsTotal   *prometheus.CounterVec
	jobDuration *prometheus.HistogramVec
	activeJobs  *prometheus.GaugeVec
}

// NewWorkerMetrics creates and registers worker metric collectors.
func NewWorkerMetrics(reg *metrics.Registry) *WorkerMetrics {
	m := &WorkerMetrics{
		jobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: telemetry.MetricWorkerJobsTotal,
			Help: "Total number of worker jobs processed.",
		}, []string{telemetry.LabelWorkerName, telemetry.LabelOutcome}),
		jobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    telemetry.MetricWorkerJobDuration,
			Help:    "Worker job execution duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{telemetry.LabelWorkerName}),
		activeJobs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: telemetry.MetricWorkerActiveJobs,
			Help: "Number of currently active worker jobs.",
		}, []string{telemetry.LabelWorkerName}),
	}

	reg.MustRegister(m.jobsTotal, m.jobDuration, m.activeJobs)
	return m
}

// ObservableWorker wraps a bootstrap.Worker to record job metrics.
type ObservableWorker struct {
	inner   bootstrap.Worker
	metrics *WorkerMetrics
}

// NewObservableWorker wraps a worker with metrics tracking.
func NewObservableWorker(inner bootstrap.Worker, wm *WorkerMetrics) bootstrap.Worker {
	return &ObservableWorker{inner: inner, metrics: wm}
}

// Name delegates to the inner worker.
func (w *ObservableWorker) Name() string {
	return w.inner.Name()
}

// Start delegates to the inner worker and tracks the active job gauge.
// worker_jobs_total 不在此计数：job 轮次口径（含单轮 outcome 与 duration）
// 由 job 执行侧的 TrackJob 统一记录，避免同一指标混入启动计数。
func (w *ObservableWorker) Start() error {
	if w.metrics != nil {
		w.metrics.activeJobs.WithLabelValues(w.inner.Name()).Inc()
	}
	return w.inner.Start()
}

// Stop delegates to the inner worker and updates the active job gauge.
func (w *ObservableWorker) Stop(ctx context.Context) error {
	err := w.inner.Stop(ctx)
	if w.metrics != nil {
		w.metrics.activeJobs.WithLabelValues(w.inner.Name()).Dec()
	}
	return err
}

// TrackJob is a helper for one-shot job execution that records timing and outcome.
func TrackJob(workerName string, wm *WorkerMetrics, fn func() error) error {
	if wm == nil {
		return fn()
	}

	start := time.Now()
	wm.activeJobs.WithLabelValues(workerName).Inc()
	defer wm.activeJobs.WithLabelValues(workerName).Dec()

	err := fn()
	duration := time.Since(start).Seconds()

	outcome := "success"
	if err != nil {
		outcome = "failure"
	}

	wm.jobsTotal.WithLabelValues(workerName, outcome).Inc()
	wm.jobDuration.WithLabelValues(workerName).Observe(duration)

	return err
}
