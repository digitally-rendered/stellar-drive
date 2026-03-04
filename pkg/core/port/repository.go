// Package port contains the primary and secondary port interfaces that define
// the boundaries of the stellar-drive core domain. Adapters (MongoDB, SQL,
// Kafka, HTTP, etc.) implement these interfaces; the service layer depends only
// on the interfaces, never on concrete implementations.
package port

import (
	"context"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// Repository is the secondary (driven) port for document persistence.
// Concrete implementations live in pkg/adapter/driven/ and are injected at
// wire-up time. All methods are schema-scoped: schemaName identifies the
// logical collection or table to operate on.
type Repository interface {
	// Create persists a new document with the given data and returns the
	// stored Document, including its generated ID, timestamps, and ETag.
	Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error)

	// FindByID retrieves the current (non-deleted) document for entityID.
	// Returns a NotFound domain error when the entity does not exist.
	FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error)

	// List executes a structured query and returns a paginated result set.
	// q may be nil, in which case the implementation returns all documents
	// using default ordering and no limit.
	List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)

	// Update applies a partial patch (data) to the document identified by
	// entityID, increments its RecordVersion, and returns the updated Document.
	// Returns a NotFound domain error when the entity does not exist.
	Update(ctx context.Context, schemaName string, entityID string, data map[string]any) (*model.Document, error)

	// Delete soft-deletes the document identified by entityID by setting its
	// DeletedAt timestamp. Returns a NotFound domain error when the entity
	// does not exist.
	Delete(ctx context.Context, schemaName string, entityID string) error

	// BulkCreate persists multiple documents in a single operation. The
	// returned slice preserves input order. Implementations may execute this
	// atomically or best-effort; callers should consult adapter documentation.
	BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error)

	// BulkUpdate applies per-item patches to multiple existing documents.
	// Items whose entity IDs do not exist are silently skipped unless the
	// implementation documents otherwise.
	BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error)

	// BulkDelete soft-deletes the documents identified by ids.
	BulkDelete(ctx context.Context, schemaName string, ids []string) error

	// EnsureIndexes creates or updates any storage indexes declared in the
	// schema definition for schemaName. It is idempotent and safe to call on
	// every startup.
	EnsureIndexes(ctx context.Context, schemaName string) error
}
