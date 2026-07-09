package handlers

import (
	"github.com/go-playground/validator/v10"
)

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ParseValidationErrors converts validator errors to user-friendly format
func ParseValidationErrors(err error) []ValidationError {
	var errors []ValidationError

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldError := range validationErrors {
			errors = append(errors, ValidationError{
				Field:   fieldError.Field(),
				Message: getErrorMessage(fieldError),
			})
		}
	}

	return errors
}

func getErrorMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fe.Field() + " is required"
	case "email":
		return "Invalid email format"
	case "min":
		return fe.Field() + " must be at least " + fe.Param() + " characters"
	case "max":
		return fe.Field() + " must not exceed " + fe.Param() + " characters"
	case "oneof":
		return fe.Field() + " must be one of: " + fe.Param()
	case "alphanum":
		return fe.Field() + " must contain only letters and numbers"
	case "url":
		return "Invalid URL format"
	case "startswith":
		return fe.Field() + " must start with " + fe.Param()
	default:
		return fe.Field() + " is invalid"
	}
}
