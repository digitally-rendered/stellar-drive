// Package service provides the default business-logic implementations of the
// port.Service interface. GenericCRUDService orchestrates schema validation,
// lifecycle event publishing, and repository delegation for all CRUD and bulk
// operations.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// GenericCRUDService implements port.Service with default behaviour.
// It validates data against schemas registered in the Registry, fires
// lifecycle events through the EventBus, and delegates persistence to the
// Repository.
//
// If a schema is not found in the Registry (e.g. dynamically defined schemas
// that have not been pre-loaded), validation is skipped rather than returning
// an error, allowing runtime flexibility.
type GenericCRUDService struct {
	repo     port.Repository
	bus      port.EventBus
	registry *schema.Registry
}

// NewGenericCRUDService constructs a GenericCRUDService with the supplied
// dependencies. All three arguments are required; passing nil will cause a
// panic on first use.
func NewGenericCRUDService(repo port.Repository, bus port.EventBus, registry *schema.Registry) *GenericCRUDService {
	return &GenericCRUDService{
		repo:     repo,
		bus:      bus,
		registry: registry,
	}
}

// ---------------------------------------------------------------------------
// Single-document operations
// ---------------------------------------------------------------------------

// Create validates input against the named schema, publishes a pre_create
// event, persists the document, then publishes a post_create event. A non-nil
// error from the pre_create event aborts the operation; post_create errors are
// silently ignored.
func (s *GenericCRUDService) Create(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error) {
	if err := s.validate(schemaName, input, false); err != nil {
		return nil, err
	}

	if err := s.publishEvent(ctx, event.PreCreate, schemaName, input, nil); err != nil {
		return nil, fmt.Errorf("pre_create event: %w", err)
	}

	doc, err := s.repo.Create(ctx, schemaName, input)
	if err != nil {
		return nil, err
	}

	// Post-event errors are informational; do not abort the response.
	_ = s.publishEvent(ctx, event.PostCreate, schemaName, input, doc)

	return doc, nil
}

// FindByID publishes a pre_get event, fetches the document by entity ID, then
// publishes a post_get event with the result.
func (s *GenericCRUDService) FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error) {
	if err := s.publishEvent(ctx, event.PreGet, schemaName, entityID, nil); err != nil {
		return nil, fmt.Errorf("pre_get event: %w", err)
	}

	doc, err := s.repo.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return nil, err
	}

	_ = s.publishEvent(ctx, event.PostGet, schemaName, entityID, doc)

	return doc, nil
}

// List normalises query pagination, publishes a pre_list event, executes the
// list query, then publishes a post_list event with the result.
func (s *GenericCRUDService) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	if q == nil {
		q = &query.Query{}
	}
	normalizeQueryPagination(q)

	if err := s.publishEvent(ctx, event.PreList, schemaName, q, nil); err != nil {
		return nil, fmt.Errorf("pre_list event: %w", err)
	}

	result, err := s.repo.List(ctx, schemaName, q)
	if err != nil {
		return nil, err
	}

	_ = s.publishEvent(ctx, event.PostList, schemaName, q, result)

	return result, nil
}

// Update performs partial (PATCH) validation of input, publishes a
// pre_update event, applies the patch, then publishes a post_update event.
func (s *GenericCRUDService) Update(ctx context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error) {
	// Partial validation: required fields are not enforced for patches.
	if err := s.validate(schemaName, input, true); err != nil {
		return nil, err
	}

	if err := s.publishEvent(ctx, event.PreUpdate, schemaName, input, nil); err != nil {
		return nil, fmt.Errorf("pre_update event: %w", err)
	}

	doc, err := s.repo.Update(ctx, schemaName, entityID, input)
	if err != nil {
		return nil, err
	}

	_ = s.publishEvent(ctx, event.PostUpdate, schemaName, input, doc)

	return doc, nil
}

// Delete publishes a pre_delete event, removes the document, then publishes a
// post_delete event.
func (s *GenericCRUDService) Delete(ctx context.Context, schemaName string, entityID string) error {
	if err := s.publishEvent(ctx, event.PreDelete, schemaName, entityID, nil); err != nil {
		return fmt.Errorf("pre_delete event: %w", err)
	}

	if err := s.repo.Delete(ctx, schemaName, entityID); err != nil {
		return err
	}

	_ = s.publishEvent(ctx, event.PostDelete, schemaName, entityID, nil)

	return nil
}

// ---------------------------------------------------------------------------
// Bulk operations
// ---------------------------------------------------------------------------

// BulkCreate validates each item in inputs, publishes a pre_bulk_create event,
// persists all documents, then publishes a post_bulk_create event.
func (s *GenericCRUDService) BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error) {
	for i, item := range inputs {
		if err := s.validate(schemaName, item, false); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}

	if err := s.publishEvent(ctx, event.PreBulkCreate, schemaName, inputs, nil); err != nil {
		return nil, fmt.Errorf("pre_bulk_create event: %w", err)
	}

	docs, err := s.repo.BulkCreate(ctx, schemaName, inputs)
	if err != nil {
		return nil, err
	}

	_ = s.publishEvent(ctx, event.PostBulkCreate, schemaName, inputs, docs)

	return docs, nil
}

// BulkUpdate validates each patch in items, publishes a pre_bulk_update event,
// applies all patches, then publishes a post_bulk_update event.
func (s *GenericCRUDService) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	for i, item := range items {
		if err := s.validate(schemaName, item.Data, true); err != nil {
			return nil, fmt.Errorf("item %d (entity %q): %w", i, item.EntityID, err)
		}
	}

	if err := s.publishEvent(ctx, event.PreBulkUpdate, schemaName, items, nil); err != nil {
		return nil, fmt.Errorf("pre_bulk_update event: %w", err)
	}

	docs, err := s.repo.BulkUpdate(ctx, schemaName, items)
	if err != nil {
		return nil, err
	}

	_ = s.publishEvent(ctx, event.PostBulkUpdate, schemaName, items, docs)

	return docs, nil
}

// BulkDelete publishes a pre_bulk_delete event, removes the identified
// documents, then publishes a post_bulk_delete event.
func (s *GenericCRUDService) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	if err := s.publishEvent(ctx, event.PreBulkDelete, schemaName, ids, nil); err != nil {
		return fmt.Errorf("pre_bulk_delete event: %w", err)
	}

	if err := s.repo.BulkDelete(ctx, schemaName, ids); err != nil {
		return err
	}

	_ = s.publishEvent(ctx, event.PostBulkDelete, schemaName, ids, nil)

	return nil
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// publishEvent builds and delivers a lifecycle event to the EventBus.
func (s *GenericCRUDService) publishEvent(ctx context.Context, t event.Type, schemaName string, input, result any) error {
	return s.bus.Publish(ctx, &event.Event{
		Type:       t,
		SchemaName: schemaName,
		Timestamp:  time.Now().UTC(),
		Input:      input,
		Result:     result,
	})
}

// validate looks up the schema definition and calls schema.ValidateData. When
// partial is true (PATCH semantics) a temporary definition is constructed with
// an empty RequiredFields slice so only supplied fields are type-checked. If
// the schema is not registered, validation is skipped (dynamic schemas).
func (s *GenericCRUDService) validate(schemaName string, data map[string]any, partial bool) error {
	def, err := s.registry.Get(schemaName, "")
	if err != nil {
		// Schema not in registry — dynamic schema; skip validation.
		return nil
	}

	if partial {
		// Shallow-copy the definition and clear required fields for patch
		// semantics: callers supply only the fields they want to change.
		patchDef := *def
		patchDef.RequiredFields = nil
		def = &patchDef
	}

	return schema.ValidateData(def, data)
}

// normalizeQueryPagination clamps the query's Limit and resets negative
// Offset values to ensure sensible pagination defaults.
func normalizeQueryPagination(q *query.Query) {
	if q.Limit <= 0 {
		q.Limit = model.DefaultLimit
	}
	if q.Limit > model.MaxLimit {
		q.Limit = model.MaxLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
}
