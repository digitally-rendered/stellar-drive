package migration

import (
	"context"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// CurrentVersionFunc returns the version every schemaName's documents should
// be migrated to before they leave the repository. Typically this is a closure
// over the schema registry's latest pointer.
type CurrentVersionFunc func(schemaName string) string

// Repository decorates a port.Repository with read-time schema-version
// migrations. Every read-returning method applies the registered migration
// chain so consumers always see documents in the current schema shape.
//
// Write paths (Create, Update, BulkCreate, BulkUpdate, Delete, BulkDelete) are
// passed through unchanged: documents are always written at the current
// schema version because the service layer populates SchemaVersion from the
// registry.
type Repository struct {
	inner      port.Repository
	registry   *Registry
	currentVer CurrentVersionFunc
}

// NewRepository wraps inner with migration-aware read behaviour. registry
// holds the per-schema migration chains; currentVer reports the current
// version for each schema. If currentVer returns "" for a schema, no
// migration is attempted for that schema's reads.
func NewRepository(inner port.Repository, registry *Registry, currentVer CurrentVersionFunc) *Repository {
	return &Repository{
		inner:      inner,
		registry:   registry,
		currentVer: currentVer,
	}
}

// Create delegates unchanged — writes happen at the current schema version.
func (r *Repository) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	return r.inner.Create(ctx, schemaName, data)
}

// FindByID fetches the document and applies any outstanding migrations before
// returning it.
func (r *Repository) FindByID(ctx context.Context, schemaName, entityID string) (*model.Document, error) {
	doc, err := r.inner.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return nil, err
	}
	if err := r.migrate(ctx, schemaName, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// List fetches a page of documents and applies migrations to every entry.
func (r *Repository) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	res, err := r.inner.List(ctx, schemaName, q)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for _, d := range res.Items {
			if err := r.migrate(ctx, schemaName, d); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// Update delegates unchanged — writes happen at the current schema version.
func (r *Repository) Update(ctx context.Context, schemaName, entityID string, data map[string]any) (*model.Document, error) {
	return r.inner.Update(ctx, schemaName, entityID, data)
}

// Delete delegates unchanged.
func (r *Repository) Delete(ctx context.Context, schemaName, entityID string) error {
	return r.inner.Delete(ctx, schemaName, entityID)
}

// BulkCreate delegates unchanged.
func (r *Repository) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	return r.inner.BulkCreate(ctx, schemaName, items)
}

// BulkUpdate delegates unchanged.
func (r *Repository) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	return r.inner.BulkUpdate(ctx, schemaName, items)
}

// BulkDelete delegates unchanged.
func (r *Repository) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	return r.inner.BulkDelete(ctx, schemaName, ids)
}

// EnsureIndexes delegates unchanged.
func (r *Repository) EnsureIndexes(ctx context.Context, schemaName string) error {
	return r.inner.EnsureIndexes(ctx, schemaName)
}

// migrate applies any outstanding migrations for schemaName to doc. When no
// current version is configured or no migrations are registered the call is a
// cheap no-op.
func (r *Repository) migrate(ctx context.Context, schemaName string, doc *model.Document) error {
	if doc == nil || r.registry == nil || r.currentVer == nil {
		return nil
	}
	target := r.currentVer(schemaName)
	if target == "" {
		return nil
	}
	return r.registry.Apply(ctx, doc, target)
}
