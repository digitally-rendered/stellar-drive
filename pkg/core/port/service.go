package port

import (
	"context"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// Service is the primary (driving) port for document business logic. It sits
// between HTTP/GraphQL handlers and the Repository, and is also the layer
// where schema validation, event publishing, and policy checks are applied.
//
// All methods are schema-scoped: schemaName selects which JSON Schema is used
// for validation and which storage collection is targeted.
type Service interface {
	// Create validates input against the named schema, fires Pre/PostCreate
	// lifecycle events, and delegates persistence to the Repository.
	Create(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error)

	// FindByID fires Pre/PostGet events and retrieves the current document for
	// entityID. Returns a NotFound domain error when the entity does not exist.
	FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error)

	// List fires Pre/PostList events and executes q against the Repository.
	// q may be nil; the implementation applies sensible defaults.
	List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)

	// Update validates input, fires Pre/PostUpdate events, and applies the
	// patch to the existing document. Returns a NotFound domain error when the
	// entity does not exist.
	Update(ctx context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error)

	// Delete fires Pre/PostDelete events and soft-deletes the document.
	// Returns a NotFound domain error when the entity does not exist.
	Delete(ctx context.Context, schemaName string, entityID string) error

	// BulkCreate validates each item in inputs, fires Pre/PostBulkCreate events,
	// and delegates to Repository.BulkCreate. The returned slice preserves
	// input order.
	BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error)

	// BulkUpdate validates each item, fires Pre/PostBulkUpdate events, and
	// delegates to Repository.BulkUpdate.
	BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error)

	// BulkDelete fires Pre/PostBulkDelete events and soft-deletes the
	// documents identified by ids.
	BulkDelete(ctx context.Context, schemaName string, ids []string) error
}
