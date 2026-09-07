package errors

import (
	"errors"
	"testing"
)

func TestAppError_NilReceiver(t *testing.T) {
	var e *AppError
	if e.Error() != "" {
		t.Fatalf("expected empty string, got %q", e.Error())
	}
	if e.Unwrap() != nil {
		t.Fatalf("expected nil unwrap, got %v", e.Unwrap())
	}
}

func TestAppError_ErrorWithoutModule(t *testing.T) {
	appErr := New(errors.New("bad input"), SeverityUser, CategoryValidation)
	expected := "[user:validation] bad input"
	if appErr.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, appErr.Error())
	}
}

func TestNew_UnknownSeverityDefaultStatus(t *testing.T) {
	appErr := New(errors.New("mystery"), Severity("bogus"), CategoryInternal)
	if appErr.HTTPStatus != 500 {
		t.Fatalf("expected 500 for unknown severity, got %d", appErr.HTTPStatus)
	}
}
