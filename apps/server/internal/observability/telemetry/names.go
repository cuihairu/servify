// Package telemetry provides unified naming conventions, context propagation,
// and structured logging helpers for observability across all modules.
package telemetry

// Metric name constants follow Prometheus naming conventions: subsystem_name_units.
const (
	// HTTP metrics
	MetricHTTPRequestTotal    = "http_requests_total"
	MetricHTTPRequestDuration = "http_request_duration_seconds"
	MetricHTTPResponseSize    = "http_response_size_bytes"

	// Business metrics
	MetricConversationsCreated = "conversations_created_total"
	MetricTicketsCreated       = "tickets_created_total"
	MetricTicketsResolved      = "tickets_resolved_total"
	MetricRoutingDecisions     = "routing_decisions_total"
	MetricAIRequestsTotal      = "ai_requests_total"
	MetricAIRequestDuration    = "ai_request_duration_seconds"
	MetricAILLMTokenUsage      = "ai_llm_tokens_total"
	MetricVoiceTransSentences  = "voice_translation_sentences_total"
	MetricRateLimitDropped     = "ratelimit_dropped_total"

	// Event bus metrics
	MetricEventBusPublished  = "eventbus_published_total"
	MetricEventBusHandled    = "eventbus_handled_total"
	MetricEventBusFailed     = "eventbus_failed_total"
	MetricEventBusDuration   = "eventbus_handle_duration_seconds"
	MetricEventBusDeadLetter = "eventbus_dead_letter_total"

	// Worker metrics
	MetricWorkerJobsTotal   = "worker_jobs_total"
	MetricWorkerJobDuration = "worker_job_duration_seconds"
	MetricWorkerActiveJobs  = "worker_active_jobs"

	// Error metrics
	MetricErrorsTotal = "errors_total"
)

// Label name constants for Prometheus metric labels and log fields.
const (
	LabelMethod        = "method"
	LabelPath          = "path"
	LabelStatusCode    = "status_code"
	LabelHandler       = "handler"
	LabelTenantID      = "tenant_id"
	LabelSessionID     = "session_id"
	LabelProvider      = "provider"
	LabelModel         = "model"
	LabelErrorCategory = "error_category"
	LabelErrorModule   = "error_module"
	LabelSeverity      = "severity"
	LabelEventType     = "event_type"
	LabelWorkerName    = "worker_name"
	LabelOutcome       = "outcome"
	LabelChannel       = "channel"
	LabelPriority      = "priority"
	LabelStrategy      = "strategy"
	LabelTokenType     = "token_type"
)

// Span name constants for OpenTelemetry tracing.
const (
	SpanHTTPRoute    = "http.route"
	SpanAIRequest    = "ai.request"
	SpanEventPublish = "eventbus.publish"
	SpanEventHandler = "eventbus.handler"
	SpanWorkerJob    = "worker.job"
)

// Log field key constants for structured logging.
const (
	FieldRequestID = "request_id"
	FieldSessionID = "session_id"
	FieldTenantID  = "tenant_id"
	FieldTraceID   = "trace_id"
	FieldSpanID    = "span_id"
)
