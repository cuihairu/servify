package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDxcIsNotFoundError(t *testing.T) {
	assert.False(t, isNotFoundError(nil))
	assert.False(t, isNotFoundError(errors.New("boom")))
	assert.True(t, isNotFoundError(errors.New("Ticket NOT FOUND ")))
}

func TestDxcIsInvalidInputError(t *testing.T) {
	assert.False(t, isInvalidInputError(nil))
	assert.False(t, isInvalidInputError(errors.New("boom")))
	assert.True(t, isInvalidInputError(errors.New("name is Required")))
	assert.True(t, isInvalidInputError(errors.New("Invalid value")))
}

func TestDxcIsConflictError(t *testing.T) {
	assert.False(t, isConflictError(nil))
	assert.False(t, isConflictError(errors.New("boom")))
	assert.True(t, isConflictError(errors.New("already exists")))
	assert.True(t, isConflictError(errors.New("user is Already an Agent")))
	assert.True(t, isConflictError(errors.New("DUPLICATE key")))
}
