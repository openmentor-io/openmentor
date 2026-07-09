package errors

import (
	"errors"
	"fmt"
)

// Common application errors with proper types for error handling

var (
	// ErrNotFound indicates a requested resource was not found
	ErrNotFound = errors.New("not found")

	// ErrAccessDenied indicates the user doesn't have permission
	ErrAccessDenied = errors.New("access denied")

	// ErrInvalidInput indicates invalid input data
	ErrInvalidInput = errors.New("invalid input")

	// ErrUnauthorized indicates missing or invalid authentication
	ErrUnauthorized = errors.New("unauthorized")

	// ErrConflict indicates a conflict with existing data
	ErrConflict = errors.New("conflict")

	// ErrInternal indicates an internal server error
	ErrInternal = errors.New("internal error")
)

// NotFoundError creates a not found error with context
func NotFoundError(resource string) error {
	return fmt.Errorf("%s %w", resource, ErrNotFound)
}

// AccessDeniedError creates an access denied error with context
func AccessDeniedError(reason string) error {
	if reason != "" {
		return fmt.Errorf("%s: %w", reason, ErrAccessDenied)
	}
	return ErrAccessDenied
}

// InvalidInputError creates an invalid input error with context
func InvalidInputError(field, reason string) error {
	return fmt.Errorf("%s: %s: %w", field, reason, ErrInvalidInput)
}

// InternalError creates an internal error with context
func InternalError(msg string) error {
	return fmt.Errorf("%s: %w", msg, ErrInternal)
}
