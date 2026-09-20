package errors

import (
	"errors"
	"testing"
)

func TestNewAppError(t *testing.T) {
	cause := errors.New("something went wrong")
	appErr := New(cause, SeverityUser, CategoryValidation,
		WithModule("ticket"),
		WithCode("TICKET_INVALID_ID"),
	)

	if appErr.Severity != SeverityUser {
		t.Fatalf("expected user severity, got %s", appErr.Severity)
	}
	if appErr.Category != CategoryValidation {
		t.Fatalf("expected validation category, got %s", appErr.Category)
	}
	if appErr.Module != "ticket" {
		t.Fatalf("expected ticket module, got %s", appErr.Module)
	}
	if appErr.Code != "TICKET_INVALID_ID" {
		t.Fatalf("expected TICKET_INVALID_ID code, got %s", appErr.Code)
	}
	if appErr.Message != "something went wrong" {
		t.Fatalf("expected default message from cause, got %s", appErr.Message)
	}
}

func TestAppError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	appErr := New(cause, SeveritySystem, CategoryInternal)

	if !errors.Is(appErr, cause) {
		t.Fatal("expected errors.Is to match root cause")
	}
}

func TestAppError_NilInput(t *testing.T) {
	appErr := New(nil, SeverityUser, CategoryValidation)
	if appErr != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestAppError_ErrorString(t *testing.T) {
	cause := errors.New("db connection failed")
	appErr := New(cause, SeverityDependency, CategoryDatabase, WithModule("infra"))
	expected := "[dependency:database] infra: db connection failed"
	if appErr.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, appErr.Error())
	}
}
