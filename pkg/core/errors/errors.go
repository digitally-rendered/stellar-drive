// Package errors defines the domain error types used throughout stellar-drive.
//
// All domain errors carry a machine-readable ErrorCode that HTTP and GraphQL
// adapters use to produce appropriate response codes. Constructors follow the
// naming convention of the operation they describe rather than the Go error
// naming convention (e.g. NotFound, not ErrNotFound) because they return rich
// struct types, not sentinel values.
package errors

import (
	"errors"
	"fmt"
)

// DomainError is the canonical error type for all business-logic failures.
// It implements the standard error interface and supports error wrapping via
// Unwrap so that errors.Is and errors.As work across the call stack.
type DomainError struct {
	// Code is a machine-readable identifier for the error category.
	Code ErrorCode `json:"code"`

	// Message is a human-readable summary suitable for API responses.
	Message string `json:"message"`

	// Details carries field-level validation messages or other structured
	// supplementary information. May be nil.
	Details []ErrorDetail `json:"details,omitempty"`

	// Cause is the underlying error that triggered this domain error, if any.
	// It is not serialised to JSON; use Unwrap to propagate it through the
	// standard errors package.
	Cause error `json:"-"`
}

// Error implements the error interface. The format is:
//
//	[CODE] message: cause
//	[CODE] message        (when Cause is nil)
func (e *DomainError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Cause.Error())
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause so that errors.Is / errors.As can
// inspect the full error chain.
func (e *DomainError) Unwrap() error {
	return e.Cause
}

// ErrorDetail carries field-level context for validation and schema errors.
type ErrorDetail struct {
	// Field is the dot-separated path to the offending field, if applicable.
	Field string `json:"field,omitempty"`

	// Message describes what is wrong with the field.
	Message string `json:"message"`

	// Code is an optional machine-readable sub-code (e.g. "required", "min_length").
	Code string `json:"code,omitempty"`
}

// ---------------------------------------------------------------------------
// Constructors
// ---------------------------------------------------------------------------

// NotFound returns a DomainError indicating that entity with the given id
// could not be found.
//
//	err := errors.NotFound("User", "abc-123")
func NotFound(entity, id string) *DomainError {
	return &DomainError{
		Code:    CodeNotFound,
		Message: fmt.Sprintf("%s with id %q not found", entity, id),
	}
}

// Conflict returns a DomainError for state-conflict situations such as
// duplicate keys or optimistic-locking failures.
func Conflict(message string) *DomainError {
	return &DomainError{
		Code:    CodeConflict,
		Message: message,
	}
}

// ValidationFailed returns a DomainError containing one or more field-level
// validation details.
func ValidationFailed(details ...ErrorDetail) *DomainError {
	return &DomainError{
		Code:    CodeValidation,
		Message: "validation failed",
		Details: details,
	}
}

// Unauthorized returns a DomainError indicating that authentication is
// required or the supplied credentials are invalid.
func Unauthorized(message string) *DomainError {
	return &DomainError{
		Code:    CodeUnauthorized,
		Message: message,
	}
}

// Forbidden returns a DomainError indicating that the authenticated caller
// does not have permission to perform the requested operation.
func Forbidden(message string) *DomainError {
	return &DomainError{
		Code:    CodeForbidden,
		Message: message,
	}
}

// Internal returns a DomainError for unexpected server-side failures. cause
// is wrapped so the full chain is preserved for logging, but is intentionally
// not exposed in the error message to avoid leaking internals.
func Internal(message string, cause error) *DomainError {
	return &DomainError{
		Code:    CodeInternal,
		Message: message,
		Cause:   cause,
	}
}

// BadRequest returns a DomainError indicating that the caller sent a
// syntactically or structurally invalid request.
func BadRequest(message string) *DomainError {
	return &DomainError{
		Code:    CodeBadRequest,
		Message: message,
	}
}

// PreconditionFailed returns a DomainError for conditional request failures
// where the If-Match ETag does not match the current resource version.
func PreconditionFailed(message string) *DomainError {
	return &DomainError{
		Code:    CodePreconditionFailed,
		Message: message,
	}
}

// SchemaNotFound returns a DomainError indicating that a JSON Schema with the
// given name is not registered.
func SchemaNotFound(name string) *DomainError {
	return &DomainError{
		Code:    CodeSchemaNotFound,
		Message: fmt.Sprintf("schema %q not found", name),
	}
}

// SchemaInvalid returns a DomainError indicating that a schema definition is
// present but fails its own validity check. details may carry per-field
// information about what is wrong.
func SchemaInvalid(name string, details ...ErrorDetail) *DomainError {
	return &DomainError{
		Code:    CodeSchemaInvalid,
		Message: fmt.Sprintf("schema %q is invalid", name),
		Details: details,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// IsNotFound reports whether err (or any error in its chain) is a DomainError
// with code CodeNotFound or CodeSchemaNotFound.
func IsNotFound(err error) bool {
	var de *DomainError
	if errors.As(err, &de) {
		return de.Code == CodeNotFound || de.Code == CodeSchemaNotFound
	}
	return false
}

// IsConflict reports whether err (or any error in its chain) is a DomainError
// with code CodeConflict.
func IsConflict(err error) bool {
	var de *DomainError
	if errors.As(err, &de) {
		return de.Code == CodeConflict
	}
	return false
}

// IsValidation reports whether err (or any error in its chain) is a
// DomainError with code CodeValidation or CodeSchemaInvalid.
func IsValidation(err error) bool {
	var de *DomainError
	if errors.As(err, &de) {
		return de.Code == CodeValidation || de.Code == CodeSchemaInvalid
	}
	return false
}
