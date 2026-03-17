package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidationError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "ValidationError matches ErrInvalidInput",
			err:    NewValidationError("email", "invalid format"),
			target: ErrInvalidInput,
			want:   true,
		},
		{
			name:   "ValidationError does not match ErrNotFound",
			err:    NewValidationError("email", "invalid format"),
			target: ErrNotFound,
			want:   false,
		},
		{
			name:   "ValidationError does not match ErrDuplicateIdempotencyKey",
			err:    NewValidationError("key", "duplicate"),
			target: ErrDuplicateIdempotencyKey,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

func TestNotFoundError_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "NotFoundError matches ErrNotFound",
			err:    NewNotFoundError("notification", "abc-123"),
			target: ErrNotFound,
			want:   true,
		},
		{
			name:   "NotFoundError does not match ErrInvalidInput",
			err:    NewNotFoundError("notification", "abc-123"),
			target: ErrInvalidInput,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

func TestValidationError_ErrorMessage(t *testing.T) {
	err := NewValidationError("email", "must be valid")
	assert.Equal(t, `validation error on field "email": must be valid`, err.Error())
}

func TestNotFoundError_ErrorMessage(t *testing.T) {
	err := NewNotFoundError("template", "tmpl-42")
	assert.Equal(t, `template with id "tmpl-42" not found`, err.Error())
}

func TestWrap(t *testing.T) {
	wrapped := Wrap(ErrNotFound, "looking up user")
	assert.True(t, errors.Is(wrapped, ErrNotFound))
	assert.Contains(t, wrapped.Error(), "looking up user")
	assert.Contains(t, wrapped.Error(), "entity not found")
}

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are distinct.
	sentinels := []error{
		ErrNotFound,
		ErrDuplicateIdempotencyKey,
		ErrInvalidInput,
		ErrRateLimited,
		ErrChannelUnavailable,
		ErrMaxRetriesExceeded,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				assert.True(t, errors.Is(a, b))
			} else {
				assert.False(t, errors.Is(a, b), "%v should not match %v", a, b)
			}
		}
	}
}
