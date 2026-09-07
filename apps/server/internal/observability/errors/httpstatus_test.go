package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestHTTPStatusFromError(t *testing.T) {
	if got := HTTPStatusFromError(nil); got != http.StatusOK {
		t.Fatalf("nil: expected 200, got %d", got)
	}
	if got := HTTPStatusFromError(errors.New("plain")); got != http.StatusInternalServerError {
		t.Fatalf("plain: expected 500, got %d", got)
	}

	user := New(errors.New("bad"), SeverityUser, CategoryValidation)
	if got := HTTPStatusFromError(user); got != http.StatusBadRequest {
		t.Fatalf("user: expected 400, got %d", got)
	}

	zeroStatus := &AppError{Err: errors.New("x"), Severity: SeverityUser, Category: CategoryValidation}
	if got := HTTPStatusFromError(zeroStatus); got != http.StatusInternalServerError {
		t.Fatalf("zero status: expected 500, got %d", got)
	}
}

func TestUserMessageFromError(t *testing.T) {
	if got := UserMessageFromError(nil); got != "" {
		t.Fatalf("nil: expected empty, got %q", got)
	}
	if got := UserMessageFromError(errors.New("plain")); got != "Internal server error" {
		t.Fatalf("plain: expected generic, got %q", got)
	}

	user := New(errors.New("dup"), SeverityUser, CategoryValidation, WithMessage("duplicate email"))
	if got := UserMessageFromError(user); got != "duplicate email" {
		t.Fatalf("user: expected message, got %q", got)
	}

	dep := New(errors.New("timeout"), SeverityDependency, CategoryAI, WithMessage("AI temporarily unavailable"))
	if got := UserMessageFromError(dep); got != "AI temporarily unavailable" {
		t.Fatalf("dependency: expected message, got %q", got)
	}

	sys := New(errors.New("nil deref"), SeveritySystem, CategoryInternal, WithMessage("boom"))
	if got := UserMessageFromError(sys); got != "Internal server error" {
		t.Fatalf("system: expected generic, got %q", got)
	}

	cfg := New(errors.New("bad creds"), SeverityConfig, CategoryAuth, WithMessage("bad creds"))
	if got := UserMessageFromError(cfg); got != "Internal server error" {
		t.Fatalf("config: expected generic, got %q", got)
	}

	emptyMsg := &AppError{Err: errors.New("e"), Severity: SeverityUser, Category: CategoryValidation}
	if got := UserMessageFromError(emptyMsg); got != "Internal server error" {
		t.Fatalf("empty message: expected generic, got %q", got)
	}
}
