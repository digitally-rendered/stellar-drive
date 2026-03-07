package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

// mockRepository records calls and returns configurable results.
type mockRepository struct {
	createFn func(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error)
	findFn   func(ctx context.Context, schemaName, entityID string) (*model.Document, error)
	listFn   func(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)
	updateFn func(ctx context.Context, schemaName, entityID string, data map[string]any) (*model.Document, error)
	deleteFn func(ctx context.Context, schemaName, entityID string) error

	// call counts for assertions
	createCalls int
	findCalls   int
	listCalls   int
	updateCalls int
	deleteCalls int
}

func (m *mockRepository) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	m.createCalls++
	if m.createFn != nil {
		return m.createFn(ctx, schemaName, data)
	}
	return newDoc(schemaName, "entity-1", data), nil
}

func (m *mockRepository) FindByID(ctx context.Context, schemaName, entityID string) (*model.Document, error) {
	m.findCalls++
	if m.findFn != nil {
		return m.findFn(ctx, schemaName, entityID)
	}
	return newDoc(schemaName, entityID, nil), nil
}

func (m *mockRepository) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	m.listCalls++
	if m.listFn != nil {
		return m.listFn(ctx, schemaName, q)
	}
	return &model.ListResult{Items: []*model.Document{}, Total: 0}, nil
}

func (m *mockRepository) Update(ctx context.Context, schemaName, entityID string, data map[string]any) (*model.Document, error) {
	m.updateCalls++
	if m.updateFn != nil {
		return m.updateFn(ctx, schemaName, entityID, data)
	}
	return newDoc(schemaName, entityID, data), nil
}

func (m *mockRepository) Delete(ctx context.Context, schemaName, entityID string) error {
	m.deleteCalls++
	if m.deleteFn != nil {
		return m.deleteFn(ctx, schemaName, entityID)
	}
	return nil
}

func (m *mockRepository) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	docs := make([]*model.Document, len(items))
	for i, item := range items {
		docs[i] = newDoc(schemaName, fmt.Sprintf("entity-%d", i+1), item)
	}
	return docs, nil
}

func (m *mockRepository) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	docs := make([]*model.Document, len(items))
	for i, item := range items {
		docs[i] = newDoc(schemaName, item.EntityID, item.Data)
	}
	return docs, nil
}

func (m *mockRepository) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	return nil
}

func (m *mockRepository) EnsureIndexes(ctx context.Context, schemaName string) error {
	return nil
}

// ---------------------------------------------------------------------------

// mockEventBus records published events and optionally returns errors.
type mockEventBus struct {
	published []*event.Event
	errFor    map[event.Type]error // return this error when publishing the given type
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		errFor: make(map[event.Type]error),
	}
}

func (b *mockEventBus) Publish(ctx context.Context, e *event.Event) error {
	b.published = append(b.published, e)
	if err, ok := b.errFor[e.Type]; ok {
		return err
	}
	return nil
}

func (b *mockEventBus) Subscribe(t event.Type, schemaName string, h event.Handler) {
	// no-op for tests
}

// publishedTypes returns the ordered list of event types that were published.
func (b *mockEventBus) publishedTypes() []event.Type {
	types := make([]event.Type, len(b.published))
	for i, e := range b.published {
		types[i] = e.Type
	}
	return types
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newDoc(schemaName, entityID string, data map[string]any) *model.Document {
	if data == nil {
		data = map[string]any{}
	}
	return &model.Document{
		ID:            "doc-" + entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		Data:          data,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

// emptyRegistry returns a registry with no schemas registered.
func emptyRegistry() *schema.Registry {
	return schema.NewRegistry()
}

// registryWithSchema returns a registry containing a simple schema.
func registryWithSchema(name string, required []string, properties map[string]any) *schema.Registry {
	reg := schema.NewRegistry()
	env := &schema.SchemaEnvelope{
		Name:    name,
		Version: "1.0.0",
		Schema: map[string]any{
			"type":       "object",
			"properties": properties,
			"required":   required,
		},
	}
	_, _ = reg.Register(env)
	return reg
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestGenericCRUDService_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schemaName string
		input      map[string]any
		registry   *schema.Registry
		// configure the repo/bus before the call
		repoFn func(*mockRepository)
		busFn  func(*mockEventBus)
		// expectations
		wantErr        bool
		wantErrContain string
		wantRepoCalls  int
		wantEvents     []event.Type
	}{
		{
			name:          "happy path: no schema in registry, skips validation",
			schemaName:    "articles",
			input:         map[string]any{"title": "Hello"},
			registry:      emptyRegistry(),
			wantRepoCalls: 1,
			wantEvents:    []event.Type{event.PreCreate, event.PostCreate},
		},
		{
			name:       "happy path: valid data passes schema validation",
			schemaName: "articles",
			input:      map[string]any{"title": "Hello", "body": "World"},
			registry: registryWithSchema("articles", []string{"title"}, map[string]any{
				"title": map[string]any{"type": "string"},
				"body":  map[string]any{"type": "string"},
			}),
			wantRepoCalls: 1,
			wantEvents:    []event.Type{event.PreCreate, event.PostCreate},
		},
		{
			name:       "validation failure: missing required field",
			schemaName: "articles",
			input:      map[string]any{"body": "no title here"},
			registry: registryWithSchema("articles", []string{"title"}, map[string]any{
				"title": map[string]any{"type": "string"},
				"body":  map[string]any{"type": "string"},
			}),
			wantErr:        true,
			wantErrContain: "required",
			wantRepoCalls:  0,
		},
		{
			name:       "pre_create event error aborts operation",
			schemaName: "articles",
			input:      map[string]any{"title": "Hello"},
			registry:   emptyRegistry(),
			busFn: func(b *mockEventBus) {
				b.errFor[event.PreCreate] = errors.New("guard rejected")
			},
			wantErr:        true,
			wantErrContain: "pre_create event",
			wantRepoCalls:  0,
			wantEvents:     []event.Type{event.PreCreate},
		},
		{
			name:       "repository error is propagated",
			schemaName: "articles",
			input:      map[string]any{"title": "Hello"},
			registry:   emptyRegistry(),
			repoFn: func(r *mockRepository) {
				r.createFn = func(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
					return nil, errors.New("db unavailable")
				}
			},
			wantErr:        true,
			wantErrContain: "db unavailable",
			wantRepoCalls:  1,
			wantEvents:     []event.Type{event.PreCreate},
		},
		{
			name:       "post_create event error does not affect result",
			schemaName: "articles",
			input:      map[string]any{"title": "Hello"},
			registry:   emptyRegistry(),
			busFn: func(b *mockEventBus) {
				b.errFor[event.PostCreate] = errors.New("post hook failed")
			},
			wantRepoCalls: 1,
			wantEvents:    []event.Type{event.PreCreate, event.PostCreate},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepository{}
			bus := newMockEventBus()

			if tc.repoFn != nil {
				tc.repoFn(repo)
			}
			if tc.busFn != nil {
				tc.busFn(bus)
			}

			reg := tc.registry
			if reg == nil {
				reg = emptyRegistry()
			}

			svc := NewGenericCRUDService(repo, bus, reg)
			doc, err := svc.Create(context.Background(), tc.schemaName, tc.input)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrContain != "" {
					assert.Contains(t, err.Error(), tc.wantErrContain)
				}
				assert.Nil(t, doc)
			} else {
				require.NoError(t, err)
				require.NotNil(t, doc)
				assert.Equal(t, tc.schemaName, doc.SchemaName)
			}

			assert.Equal(t, tc.wantRepoCalls, repo.createCalls, "unexpected repository create call count")

			if tc.wantEvents != nil {
				assert.Equal(t, tc.wantEvents, bus.publishedTypes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FindByID tests
// ---------------------------------------------------------------------------

func TestGenericCRUDService_FindByID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		entityID       string
		repoFn         func(*mockRepository)
		busFn          func(*mockEventBus)
		wantErr        bool
		wantErrContain string
		wantEvents     []event.Type
	}{
		{
			name:       "happy path",
			entityID:   "abc-123",
			wantEvents: []event.Type{event.PreGet, event.PostGet},
		},
		{
			name:     "pre_get error aborts operation",
			entityID: "abc-123",
			busFn: func(b *mockEventBus) {
				b.errFor[event.PreGet] = errors.New("blocked")
			},
			wantErr:        true,
			wantErrContain: "pre_get event",
			wantEvents:     []event.Type{event.PreGet},
		},
		{
			name:     "repository not-found propagated",
			entityID: "missing",
			repoFn: func(r *mockRepository) {
				r.findFn = func(_ context.Context, _, _ string) (*model.Document, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr:        true,
			wantErrContain: "not found",
			wantEvents:     []event.Type{event.PreGet},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepository{}
			bus := newMockEventBus()

			if tc.repoFn != nil {
				tc.repoFn(repo)
			}
			if tc.busFn != nil {
				tc.busFn(bus)
			}

			svc := NewGenericCRUDService(repo, bus, emptyRegistry())
			doc, err := svc.FindByID(context.Background(), "articles", tc.entityID)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContain)
				assert.Nil(t, doc)
			} else {
				require.NoError(t, err)
				require.NotNil(t, doc)
			}

			if tc.wantEvents != nil {
				assert.Equal(t, tc.wantEvents, bus.publishedTypes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

func TestGenericCRUDService_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inputQuery *query.Query
		busFn      func(*mockEventBus)
		wantErr    bool
		wantLimit  int // expected normalised limit
		wantEvents []event.Type
	}{
		{
			name:       "nil query is normalised to defaults",
			inputQuery: nil,
			wantLimit:  model.DefaultLimit,
			wantEvents: []event.Type{event.PreList, event.PostList},
		},
		{
			name:       "over-limit query is clamped",
			inputQuery: &query.Query{Limit: 9999},
			wantLimit:  model.MaxLimit,
			wantEvents: []event.Type{event.PreList, event.PostList},
		},
		{
			name:       "zero limit is replaced with default",
			inputQuery: &query.Query{Limit: 0},
			wantLimit:  model.DefaultLimit,
			wantEvents: []event.Type{event.PreList, event.PostList},
		},
		{
			name:       "pre_list error aborts",
			inputQuery: nil,
			busFn: func(b *mockEventBus) {
				b.errFor[event.PreList] = errors.New("list blocked")
			},
			wantErr:    true,
			wantEvents: []event.Type{event.PreList},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepository{}
			bus := newMockEventBus()

			if tc.busFn != nil {
				tc.busFn(bus)
			}

			// Capture the query received by the repo so we can inspect normalisation.
			var capturedQuery *query.Query
			repo.listFn = func(_ context.Context, _ string, q *query.Query) (*model.ListResult, error) {
				capturedQuery = q
				return &model.ListResult{}, nil
			}

			svc := NewGenericCRUDService(repo, bus, emptyRegistry())
			result, err := svc.List(context.Background(), "articles", tc.inputQuery)

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.NotNil(t, capturedQuery)
				assert.Equal(t, tc.wantLimit, capturedQuery.Limit)
			}

			if tc.wantEvents != nil {
				assert.Equal(t, tc.wantEvents, bus.publishedTypes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Update tests
// ---------------------------------------------------------------------------

func TestGenericCRUDService_Update(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		entityID       string
		input          map[string]any
		registry       *schema.Registry
		busFn          func(*mockEventBus)
		repoFn         func(*mockRepository)
		wantErr        bool
		wantErrContain string
		wantEvents     []event.Type
		wantRepoCalls  int
	}{
		{
			name:     "happy path: partial update skips required-field check",
			entityID: "entity-1",
			// Only body is provided; title is required but we are PATCHing.
			input: map[string]any{"body": "updated body"},
			registry: registryWithSchema("articles", []string{"title"}, map[string]any{
				"title": map[string]any{"type": "string"},
				"body":  map[string]any{"type": "string"},
			}),
			wantRepoCalls: 1,
			wantEvents:    []event.Type{event.PreUpdate, event.PostUpdate},
		},
		{
			name:     "type error in partial patch is caught",
			entityID: "entity-1",
			input:    map[string]any{"title": 12345}, // wrong type
			registry: registryWithSchema("articles", []string{"title"}, map[string]any{
				"title": map[string]any{"type": "string"},
			}),
			wantErr:        true,
			wantErrContain: "validation",
			wantRepoCalls:  0,
		},
		{
			name:     "pre_update error aborts",
			entityID: "entity-1",
			input:    map[string]any{"body": "x"},
			registry: emptyRegistry(),
			busFn: func(b *mockEventBus) {
				b.errFor[event.PreUpdate] = errors.New("update blocked")
			},
			wantErr:        true,
			wantErrContain: "pre_update event",
			wantRepoCalls:  0,
			wantEvents:     []event.Type{event.PreUpdate},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepository{}
			bus := newMockEventBus()

			if tc.busFn != nil {
				tc.busFn(bus)
			}
			if tc.repoFn != nil {
				tc.repoFn(repo)
			}

			svc := NewGenericCRUDService(repo, bus, tc.registry)
			doc, err := svc.Update(context.Background(), "articles", tc.entityID, tc.input)

			if tc.wantErr {
				require.Error(t, err)
				if tc.wantErrContain != "" {
					assert.Contains(t, err.Error(), tc.wantErrContain)
				}
				assert.Nil(t, doc)
			} else {
				require.NoError(t, err)
				require.NotNil(t, doc)
			}

			assert.Equal(t, tc.wantRepoCalls, repo.updateCalls)

			if tc.wantEvents != nil {
				assert.Equal(t, tc.wantEvents, bus.publishedTypes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestGenericCRUDService_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		entityID       string
		busFn          func(*mockEventBus)
		repoFn         func(*mockRepository)
		wantErr        bool
		wantErrContain string
		wantEvents     []event.Type
		wantRepoCalls  int
	}{
		{
			name:          "happy path",
			entityID:      "entity-1",
			wantRepoCalls: 1,
			wantEvents:    []event.Type{event.PreDelete, event.PostDelete},
		},
		{
			name:     "pre_delete error aborts",
			entityID: "entity-1",
			busFn: func(b *mockEventBus) {
				b.errFor[event.PreDelete] = errors.New("delete blocked")
			},
			wantErr:        true,
			wantErrContain: "pre_delete event",
			wantRepoCalls:  0,
			wantEvents:     []event.Type{event.PreDelete},
		},
		{
			name:     "repository error is propagated",
			entityID: "entity-1",
			repoFn: func(r *mockRepository) {
				r.deleteFn = func(_ context.Context, _, _ string) error {
					return errors.New("delete failed")
				}
			},
			wantErr:        true,
			wantErrContain: "delete failed",
			wantRepoCalls:  1,
			wantEvents:     []event.Type{event.PreDelete},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepository{}
			bus := newMockEventBus()

			if tc.busFn != nil {
				tc.busFn(bus)
			}
			if tc.repoFn != nil {
				tc.repoFn(repo)
			}

			svc := NewGenericCRUDService(repo, bus, emptyRegistry())
			err := svc.Delete(context.Background(), "articles", tc.entityID)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContain)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantRepoCalls, repo.deleteCalls)

			if tc.wantEvents != nil {
				assert.Equal(t, tc.wantEvents, bus.publishedTypes())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeQueryPagination unit tests
// ---------------------------------------------------------------------------

func TestNormalizeQueryPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      query.Query
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "zero limit set to default",
			input:      query.Query{Limit: 0},
			wantLimit:  model.DefaultLimit,
			wantOffset: 0,
		},
		{
			name:       "negative limit set to default",
			input:      query.Query{Limit: -5},
			wantLimit:  model.DefaultLimit,
			wantOffset: 0,
		},
		{
			name:       "over-max limit clamped to max",
			input:      query.Query{Limit: model.MaxLimit + 1},
			wantLimit:  model.MaxLimit,
			wantOffset: 0,
		},
		{
			name:       "valid limit preserved",
			input:      query.Query{Limit: 42},
			wantLimit:  42,
			wantOffset: 0,
		},
		{
			name:       "negative offset reset to zero",
			input:      query.Query{Limit: 10, Offset: -3},
			wantLimit:  10,
			wantOffset: 0,
		},
		{
			name:       "positive offset preserved",
			input:      query.Query{Limit: 10, Offset: 5},
			wantLimit:  10,
			wantOffset: 5,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := tc.input
			normalizeQueryPagination(&q)
			assert.Equal(t, tc.wantLimit, q.Limit)
			assert.Equal(t, tc.wantOffset, q.Offset)
		})
	}
}

// ---------------------------------------------------------------------------
// NextVersion unit tests
// ---------------------------------------------------------------------------

func TestNextVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current VersionInfo
		want    VersionInfo
	}{
		{
			name:    "increments record version",
			current: VersionInfo{EntityID: "e-1", RecordVersion: 3, SchemaVersion: "1.0.0"},
			want:    VersionInfo{EntityID: "e-1", RecordVersion: 4, SchemaVersion: "1.0.0"},
		},
		{
			name:    "zero version becomes 1",
			current: VersionInfo{EntityID: "e-2", RecordVersion: 0, SchemaVersion: "2.0.0"},
			want:    VersionInfo{EntityID: "e-2", RecordVersion: 1, SchemaVersion: "2.0.0"},
		},
		{
			name:    "entity ID and schema version are carried over unchanged",
			current: VersionInfo{EntityID: "my-entity", RecordVersion: 99, SchemaVersion: "v3"},
			want:    VersionInfo{EntityID: "my-entity", RecordVersion: 100, SchemaVersion: "v3"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NextVersion(&tc.current)
			assert.Equal(t, tc.want.EntityID, got.EntityID)
			assert.Equal(t, tc.want.RecordVersion, got.RecordVersion)
			assert.Equal(t, tc.want.SchemaVersion, got.SchemaVersion)
			// Original must not be mutated.
			assert.Equal(t, tc.current.RecordVersion, tc.current.RecordVersion)
		})
	}
}
