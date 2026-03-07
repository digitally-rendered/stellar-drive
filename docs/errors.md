# Error System

Stellar-drive uses structured domain errors throughout the core and adapter layers. Every error carries a machine-readable code, a human-readable message, optional field-level details, and an optional wrapped cause. HTTP and GraphQL adapters translate these codes into appropriate response status codes automatically.

All error types and constructors live in `pkg/core/errors/`.

## DomainError Struct

`DomainError` is the canonical error type for all business-logic failures:

```go
type DomainError struct {
    Code    ErrorCode     `json:"code"`
    Message string        `json:"message"`
    Details []ErrorDetail `json:"details,omitempty"`
    Cause   error         `json:"-"`
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Code` | `ErrorCode` | Machine-readable identifier for the error category |
| `Message` | `string` | Human-readable summary suitable for API responses |
| `Details` | `[]ErrorDetail` | Field-level validation messages or supplementary info (may be nil) |
| `Cause` | `error` | Underlying error that triggered this domain error (not serialised to JSON) |

`DomainError` implements the standard `error` interface. The `Error()` method formats as:

```
[CODE] message: cause
[CODE] message          (when Cause is nil)
```

### ErrorDetail Struct

`ErrorDetail` carries field-level context for validation and schema errors:

```go
type ErrorDetail struct {
    Field   string `json:"field,omitempty"`
    Message string `json:"message"`
    Code    string `json:"code,omitempty"`
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Field` | `string` | Dot-separated path to the offending field (e.g. `"category.name"`) |
| `Message` | `string` | Description of what is wrong with the field |
| `Code` | `string` | Optional machine-readable sub-code (e.g. `"required"`, `"min_length"`) |

## Error Codes and HTTP Status Mapping

Each `ErrorCode` maps to an HTTP status code via the `HTTPStatus()` method. Unmapped codes fall back to 500 Internal Server Error.

| ErrorCode Constant | String Value | HTTP Status | Description |
|--------------------|-------------|-------------|-------------|
| `CodeNotFound` | `NOT_FOUND` | 404 | Requested resource does not exist |
| `CodeConflict` | `CONFLICT` | 409 | State conflict (duplicate resource, optimistic lock failure) |
| `CodeValidation` | `VALIDATION_FAILED` | 422 | Input failed validation rules |
| `CodeUnauthorized` | `UNAUTHORIZED` | 401 | Missing or invalid authentication credentials |
| `CodeForbidden` | `FORBIDDEN` | 403 | Authenticated caller lacks permission |
| `CodeBadRequest` | `BAD_REQUEST` | 400 | Malformed request (bad JSON, invalid query params) |
| `CodeInternal` | `INTERNAL_ERROR` | 500 | Unexpected server-side error |
| `CodeSchemaNotFound` | `SCHEMA_NOT_FOUND` | 404 | Named JSON Schema not registered |
| `CodeSchemaInvalid` | `SCHEMA_INVALID` | 422 | Schema definition failed validation |

The mapping is implemented in `pkg/core/errors/codes.go`:

```go
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
```

## Constructors

Each error code has a corresponding constructor function that returns a `*DomainError`:

### NotFound

Returns a `CodeNotFound` error for a missing entity.

```go
err := errors.NotFound("Pet", "abc-123")
// [NOT_FOUND] Pet with id "abc-123" not found
```

### Conflict

Returns a `CodeConflict` error for state conflicts such as duplicate keys or optimistic-locking failures.

```go
err := errors.Conflict("document version conflict: expected 3, got 2")
// [CONFLICT] document version conflict: expected 3, got 2
```

### ValidationFailed

Returns a `CodeValidation` error containing one or more field-level validation details.

```go
err := errors.ValidationFailed(
    errors.ErrorDetail{Field: "name", Message: "is required", Code: "required"},
    errors.ErrorDetail{Field: "status", Message: "must be one of: available, pending, sold", Code: "enum"},
)
// [VALIDATION_FAILED] validation failed
```

### Unauthorized

Returns a `CodeUnauthorized` error indicating that authentication is required or credentials are invalid.

```go
err := errors.Unauthorized("invalid or expired token")
// [UNAUTHORIZED] invalid or expired token
```

### Forbidden

Returns a `CodeForbidden` error indicating the authenticated caller lacks permission.

```go
err := errors.Forbidden("insufficient permissions to delete this resource")
// [FORBIDDEN] insufficient permissions to delete this resource
```

### Internal

Returns a `CodeInternal` error for unexpected server-side failures. The `cause` is wrapped for logging but not exposed in the error message to avoid leaking internals.

```go
err := errors.Internal("failed to persist document", dbErr)
// [INTERNAL_ERROR] failed to persist document
// errors.Unwrap(err) == dbErr
```

### BadRequest

Returns a `CodeBadRequest` error for syntactically or structurally invalid requests.

```go
err := errors.BadRequest("request body is not valid JSON")
// [BAD_REQUEST] request body is not valid JSON
```

### SchemaNotFound

Returns a `CodeSchemaNotFound` error for a missing JSON Schema in the registry.

```go
err := errors.SchemaNotFound("invoice")
// [SCHEMA_NOT_FOUND] schema "invoice" not found
```

### SchemaInvalid

Returns a `CodeSchemaInvalid` error for a schema definition that fails validation. Accepts optional `ErrorDetail` values.

```go
err := errors.SchemaInvalid("pet",
    errors.ErrorDetail{Field: "schema.properties.name", Message: "missing type", Code: "required_property"},
)
// [SCHEMA_INVALID] schema "pet" is invalid
```

## Error Wrapping and Unwrap

`DomainError` supports the standard Go error wrapping protocol. The `Unwrap()` method returns the underlying `Cause`, enabling `errors.Is` and `errors.As` to inspect the full error chain:

```go
dbErr := fmt.Errorf("connection refused")
domainErr := errors.Internal("database unavailable", dbErr)

// Standard error chain inspection works:
errors.Is(domainErr, dbErr)        // true
errors.Unwrap(domainErr) == dbErr  // true

// Type assertion via errors.As:
var de *errors.DomainError
if errors.As(err, &de) {
    fmt.Println(de.Code)    // INTERNAL_ERROR
    fmt.Println(de.Message) // database unavailable
}
```

### Error Wrapping Convention

Throughout stellar-drive, errors are wrapped with context using `fmt.Errorf`:

```go
doc, err := repo.FindByID(ctx, schemaName, id)
if err != nil {
    return nil, fmt.Errorf("service.FindByID: %w", err)
}
```

This preserves the full error chain while adding call-site context for debugging.

## Helper Functions

Three helper functions simplify error type checking across the call stack:

### IsNotFound

Reports whether `err` (or any error in its chain) is a `DomainError` with code `CodeNotFound` or `CodeSchemaNotFound`.

```go
doc, err := svc.FindByID(ctx, "pet", id)
if errors.IsNotFound(err) {
    // Return 404 response
}
```

### IsConflict

Reports whether `err` (or any error in its chain) is a `DomainError` with code `CodeConflict`.

```go
doc, err := svc.Create(ctx, "pet", data)
if errors.IsConflict(err) {
    // Return 409 response
}
```

### IsValidation

Reports whether `err` (or any error in its chain) is a `DomainError` with code `CodeValidation` or `CodeSchemaInvalid`.

```go
doc, err := svc.Create(ctx, "pet", data)
if errors.IsValidation(err) {
    // Return 422 response with field-level details
}
```

## API Error Response Format

When the REST adapter catches a `DomainError`, it serialises it into a JSON envelope with the appropriate HTTP status code. The response body mirrors the `DomainError` struct:

### Simple Error (No Details)

Request:
```bash
curl http://localhost:8080/api/v1/pet/nonexistent-id
```

Response (404 Not Found):
```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Pet with id \"nonexistent-id\" not found"
  }
}
```

### Validation Error (With Details)

Request:
```bash
curl -X POST http://localhost:8080/api/v1/pet/ \
  -H "Content-Type: application/json" \
  -d '{"status": "invalid"}'
```

Response (422 Unprocessable Entity):
```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "validation failed",
    "details": [
      {
        "field": "name",
        "message": "is required",
        "code": "required"
      },
      {
        "field": "photo_urls",
        "message": "is required",
        "code": "required"
      },
      {
        "field": "status",
        "message": "must be one of: available, pending, sold",
        "code": "enum"
      }
    ]
  }
}
```

### Internal Error

Internal errors intentionally hide the underlying cause to avoid leaking implementation details. The cause is logged server-side for debugging.

Response (500 Internal Server Error):
```json
{
  "error": {
    "code": "INTERNAL_ERROR",
    "message": "failed to persist document"
  }
}
```

## Creating Custom Errors

You can create custom domain errors by constructing `DomainError` directly or by defining new `ErrorCode` constants in your application:

```go
const CodeRateLimited ErrorCode = "RATE_LIMITED"

func RateLimited(retryAfter time.Duration) *DomainError {
    return &DomainError{
        Code:    CodeRateLimited,
        Message: fmt.Sprintf("rate limit exceeded, retry after %s", retryAfter),
    }
}
```

Note that custom error codes will map to 500 by default unless you extend the `HTTPStatus()` mapping in your adapter layer.

## Related Documentation

- **[Architecture](architecture.md)** -- Error handling in the request/response flow
- **[Events](events.md)** -- Pre-event handler errors abort operations using these domain errors
- **[Registry](registry.md)** -- Guard functions return domain errors to deny requests
