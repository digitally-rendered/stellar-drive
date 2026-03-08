// Package registry provides a thread-safe store for custom handler, guard,
// validator, and transform functions keyed by schema name, operation, and
// scope. It enables per-schema, per-version, and per-channel overrides of the
// default CRUD behaviour without modifying the core service layer.
package registry

import (
	"context"
	"net/http"
)

// Scope qualifies the applicability of a registered function. The zero value
// (both fields empty) matches every request regardless of version or channel.
type Scope struct {
	// Version constrains matching to a specific schema version string (e.g.
	// "1.0.0"). An empty string matches any version.
	Version string

	// Channel constrains matching to a specific transport channel (e.g.
	// "rest", "graphql"). An empty string matches any channel.
	Channel string
}

// GuardFunc decides whether a request should be allowed to proceed. Returning
// a non-nil error denies the request; the error is propagated directly to the
// caller. All registered guards for a schema+operation are evaluated in
// specificity order; the first error short-circuits further evaluation.
type GuardFunc func(ctx context.Context, r *http.Request) error

// ValidatorFunc performs custom validation on input data before the service
// layer applies schema validation. Returning a non-nil error aborts the
// operation.
type ValidatorFunc func(ctx context.Context, schemaName string, data map[string]any) error

// TransformFunc mutates or enriches input data before it is passed to the
// repository. The returned map replaces the original data; returning nil is
// treated as an empty map.
type TransformFunc func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error)

// HandlerFunc is a complete HTTP handler override for a schema+operation. When
// a HandlerFunc is registered and resolves for a request, the default service
// pipeline is bypassed entirely.
type HandlerFunc func(w http.ResponseWriter, r *http.Request)

// Operation represents a named CRUD operation that can have overrides.
type Operation string

const (
	// OpCreate identifies the create (POST) operation.
	OpCreate Operation = "create"
	// OpGet identifies the single-document fetch (GET /:id) operation.
	OpGet Operation = "get"
	// OpList identifies the collection query (GET /) operation.
	OpList Operation = "list"
	// OpUpdate identifies the partial-update (PATCH /:id) operation.
	OpUpdate Operation = "update"
	// OpDelete identifies the delete (DELETE /:id) operation.
	OpDelete Operation = "delete"
	// OpBulkCreate identifies the bulk-create (POST /_bulk) operation.
	OpBulkCreate Operation = "bulk_create"
	// OpBulkUpdate identifies the bulk-update (PATCH /_bulk) operation.
	OpBulkUpdate Operation = "bulk_update"
	// OpBulkDelete identifies the bulk-delete (DELETE /_bulk) operation.
	OpBulkDelete Operation = "bulk_delete"
)
