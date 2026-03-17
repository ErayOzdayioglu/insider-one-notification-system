package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors for common domain failure scenarios.
var (
	ErrNotFound               = errors.New("entity not found")
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
	ErrInvalidInput           = errors.New("invalid input")
	ErrRateLimited            = errors.New("rate limited")
	ErrChannelUnavailable     = errors.New("channel unavailable")
	ErrMaxRetriesExceeded     = errors.New("maximum retries exceeded")
)

// ValidationError describes a validation failure on a specific field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field %q: %s", e.Field, e.Message)
}

// Is allows ValidationError to match ErrInvalidInput via errors.Is.
func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidInput
}

// NewValidationError creates a ValidationError for the given field.
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// NotFoundError describes a missing entity lookup.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with id %q not found", e.Entity, e.ID)
}

// Is allows NotFoundError to match ErrNotFound via errors.Is.
func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// NewNotFoundError creates a NotFoundError for the given entity and ID.
func NewNotFoundError(entity, id string) *NotFoundError {
	return &NotFoundError{Entity: entity, ID: id}
}

// Wrap wraps a sentinel domain error with additional context.
func Wrap(sentinel error, msg string) error {
	return fmt.Errorf("%s: %w", msg, sentinel)
}
