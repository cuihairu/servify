package telemetry

import (
	"context"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestFieldsFromContext_AllFields(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-1")
	ctx = WithSessionID(ctx, "sess-2")
	ctx = WithTenantID(ctx, "tenant-3")

	fields := FieldsFromContext(ctx)

	if fields[FieldRequestID] != "req-1" {
		t.Fatalf("expected req-1, got %v", fields[FieldRequestID])
	}
	if fields[FieldSessionID] != "sess-2" {
		t.Fatalf("expected sess-2, got %v", fields[FieldSessionID])
	}
	if fields[FieldTenantID] != "tenant-3" {
		t.Fatalf("expected tenant-3, got %v", fields[FieldTenantID])
	}
}

func TestFieldsFromContext_PartialFields(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-1")

	fields := FieldsFromContext(ctx)

	if fields[FieldRequestID] != "req-1" {
		t.Fatalf("expected req-1, got %v", fields[FieldRequestID])
	}
	if _, exists := fields[FieldSessionID]; exists {
		t.Fatal("expected session_id to be absent")
	}
	if _, exists := fields[FieldTenantID]; exists {
		t.Fatal("expected tenant_id to be absent")
	}
}

func TestFieldsFromContext_Empty(t *testing.T) {
	fields := FieldsFromContext(context.Background())
	if len(fields) != 0 {
		t.Fatalf("expected 0 fields, got %d", len(fields))
	}
}

func TestLoggerWithRequestID(t *testing.T) {
	logger := logrus.New()
	entry := LoggerWithRequestID(logger, "req-abc")

	if entry.Data[FieldRequestID] != "req-abc" {
		t.Fatalf("expected req-abc, got %v", entry.Data[FieldRequestID])
	}
}

func TestLoggerWithFields(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	ctx := WithRequestID(context.Background(), "req-9")
	entry := LoggerWithFields(logger, ctx)
	if entry.Data[FieldRequestID] != "req-9" {
		t.Fatalf("entry fields = %+v", entry.Data)
	}
}

func TestTraceAndSpanIDsWithActiveSpan(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	if traceID := TraceIDFromContext(ctx); traceID == "" {
		t.Fatal("expected non-empty trace id")
	}
	if spanID := SpanIDFromContext(ctx); spanID == "" {
		t.Fatal("expected non-empty span id")
	}
}

func TestTraceAndSpanIDsNilContext(t *testing.T) {
	if got := TraceIDFromContext(nil); got != "" {
		t.Fatalf("nil context trace id = %q", got)
	}
	if got := SpanIDFromContext(nil); got != "" {
		t.Fatalf("nil context span id = %q", got)
	}
}

func TestFieldsFromContextWithSpan(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	fields := FieldsFromContext(ctx)
	if fields[FieldTraceID] == nil || fields[FieldSpanID] == nil {
		t.Fatalf("expected trace/span fields, got %+v", fields)
	}
}

func TestTraceAndSpanIDsVariantsDirect(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "direct-span")
	defer span.End()

	if traceID := TraceIDFromContext(ctx); traceID == "" {
		t.Fatal("expected non-empty trace id")
	}
	if spanID := SpanIDFromContext(ctx); spanID == "" {
		t.Fatal("expected non-empty span id")
	}
}
