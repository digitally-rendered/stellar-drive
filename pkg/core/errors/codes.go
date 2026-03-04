package errors

import "net/http"

// ErrorCode is a machine-readable string that identifies the category of a
// domain error. HTTP adapters map these codes to appropriate status codes via
// HTTPStatus.
type ErrorCode string

const (
	// CodeNotFound indicates that a requested resource does not exist.
	CodeNotFound ErrorCode = "NOT_FOUND"

	// CodeConflict indicates a state conflict, e.g. a duplicate resource.
	CodeConflict ErrorCode = "CONFLICT"

	// CodeValidation indicates that input failed validation rules.
	CodeValidation ErrorCode = "VALIDATION_FAILED"

	// CodeUnauthorized indicates missing or invalid authentication credentials.
	CodeUnauthorized ErrorCode = "UNAUTHORIZED"

	// CodeForbidden indicates the caller is authenticated but lacks permission.
	CodeForbidden ErrorCode = "FORBIDDEN"

	// CodeInternal indicates an unexpected server-side error.
	CodeInternal ErrorCode = "INTERNAL_ERROR"

	// CodeBadRequest indicates that the request itself is malformed.
	CodeBadRequest ErrorCode = "BAD_REQUEST"

	// CodeSchemaNotFound indicates that a named JSON Schema could not be found
	// in the registry.
	CodeSchemaNotFound ErrorCode = "SCHEMA_NOT_FOUND"

	// CodeSchemaInvalid indicates that a schema definition failed validation.
	CodeSchemaInvalid ErrorCode = "SCHEMA_INVALID"
)

// HTTPStatus maps an ErrorCode to the most appropriate HTTP status code.
// Unmapped codes fall back to 500 Internal Server Error.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeNotFound, CodeSchemaNotFound:
		return http.StatusNotFound // 404
	case CodeConflict:
		return http.StatusConflict // 409
	case CodeValidation, CodeSchemaInvalid:
		return http.StatusUnprocessableEntity // 422
	case CodeUnauthorized:
		return http.StatusUnauthorized // 401
	case CodeForbidden:
		return http.StatusForbidden // 403
	case CodeBadRequest:
		return http.StatusBadRequest // 400
	case CodeInternal:
		return http.StatusInternalServerError // 500
	default:
		return http.StatusInternalServerError // 500
	}
}
