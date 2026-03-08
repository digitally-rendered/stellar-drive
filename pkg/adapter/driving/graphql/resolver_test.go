package graphql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gql "github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	schemapkg "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// mockService — in-memory port.Service implementation
// ---------------------------------------------------------------------------

type mockService struct {
	mu      sync.RWMutex
	docs    map[string]*model.Document
	counter int
}

func newMockService() *mockService {
	return &mockService{
		docs: make(map[string]*model.Document),
	}
}

func (m *mockService) nextID() string {
	m.counter++
	return fmt.Sprintf("entity-%d", m.counter)
}

func (m *mockService) Create(_ context.Context, schemaName string, input map[string]any) (*model.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	entityID := m.nextID()
	doc := &model.Document{
		ID:            "id-" + entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          copyMap(input),
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          fmt.Sprintf("etag-%s", entityID),
	}
	m.docs[entityID] = doc
	return doc, nil
}

func (m *mockService) FindByID(_ context.Context, schemaName string, entityID string) (*model.Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.docs[entityID]
	if !ok {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}
	return doc, nil
}

func (m *mockService) List(_ context.Context, _ string, _ *query.Query) (*model.ListResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*model.Document, 0, len(m.docs))
	for _, doc := range m.docs {
		items = append(items, doc)
	}
	return &model.ListResult{
		Items:   items,
		Total:   int64(len(items)),
		HasMore: false,
	}, nil
}

func (m *mockService) Update(_ context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[entityID]
	if !ok {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}

	// Merge input into a copy of the existing data.
	merged := copyMap(doc.Data)
	for k, v := range input {
		merged[k] = v
	}

	updated := &model.Document{
		ID:            doc.ID,
		EntityID:      doc.EntityID,
		SchemaName:    doc.SchemaName,
		RecordVersion: doc.RecordVersion + 1,
		SchemaVersion: doc.SchemaVersion,
		Data:          merged,
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     time.Now().UTC(),
		ETag:          fmt.Sprintf("etag-%s-v%d", entityID, doc.RecordVersion+1),
	}
	m.docs[entityID] = updated
	return updated, nil
}

func (m *mockService) Delete(_ context.Context, schemaName string, entityID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.docs[entityID]; !ok {
		return coreerrors.NotFound(schemaName, entityID)
	}
	delete(m.docs, entityID)
	return nil
}

func (m *mockService) BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(inputs))
	for _, input := range inputs {
		doc, err := m.Create(ctx, schemaName, input)
		if err != nil {
			return docs, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (m *mockService) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := m.Update(ctx, schemaName, item.EntityID, item.Data)
		if err != nil {
			return docs, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (m *mockService) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	for _, id := range ids {
		if err := m.Delete(ctx, schemaName, id); err != nil {
			return err
		}
	}
	return nil
}

// copyMap returns a shallow copy of m to avoid data races between the caller
// and the stored document.
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// petEnvelope returns a SchemaEnvelope for the "pet" schema used across tests.
func petEnvelope() *schemapkg.SchemaEnvelope {
	return &schemapkg.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"age":    map[string]any{"type": "integer"},
				"active": map[string]any{"type": "boolean"},
			},
			"required": []any{"name"},
		},
		Active: true,
	}
}

// setupTestSchema builds a full gql.Schema backed by a fresh mockService.
// It returns both the schema and the underlying mock so individual tests can
// pre-populate state or inspect calls.
func setupTestSchema(t *testing.T) (gql.Schema, *mockService) {
	t.Helper()

	reg := schemapkg.NewRegistry()
	_, err := reg.Register(petEnvelope())
	require.NoError(t, err)

	mock := newMockService()
	bus := event.NewBus()
	ctr := container.New(
		container.WithDefaultService(mock),
		container.WithEventBus(bus),
	)

	schema, err := BuildSchema(reg, ctr)
	require.NoError(t, err)
	return schema, mock
}

// doQuery executes a GraphQL request string against schema and returns the
// result. Context cancellation, if needed, should be handled by the caller.
func doQuery(schema gql.Schema, query string) *gql.Result {
	return gql.Do(gql.Params{
		Schema:        schema,
		RequestString: query,
		Context:       context.Background(),
	})
}

// doQueryWithVars executes a GraphQL request string with the provided
// variable map. Using variables ensures the graphql-go runtime coerces
// JSON scalars to map[string]any before the resolver sees them, which
// matches the production path where clients send JSON bodies.
func doQueryWithVars(schema gql.Schema, query string, vars map[string]any) *gql.Result {
	return gql.Do(gql.Params{
		Schema:         schema,
		RequestString:  query,
		VariableValues: vars,
		Context:        context.Background(),
	})
}

// ---------------------------------------------------------------------------
// BuildSchema tests
// ---------------------------------------------------------------------------

func TestBuildSchema_WithSchemas(t *testing.T) {
	reg := schemapkg.NewRegistry()
	_, err := reg.Register(petEnvelope())
	require.NoError(t, err)

	mock := newMockService()
	ctr := container.New(container.WithDefaultService(mock))

	schema, err := BuildSchema(reg, ctr)
	require.NoError(t, err)

	// A zero-value gql.Schema means construction succeeded; checking the
	// query type is not nil is the canonical proof.
	assert.NotNil(t, schema.QueryType(), "schema must have a Query type")
}

func TestBuildSchema_EmptyRegistry(t *testing.T) {
	reg := schemapkg.NewRegistry()
	ctr := container.New()

	schema, err := BuildSchema(reg, ctr)
	require.NoError(t, err)

	qt := schema.QueryType()
	require.NotNil(t, qt)
	_, hasEmpty := qt.Fields()["_empty"]
	assert.True(t, hasEmpty, "empty registry must produce an _empty query placeholder")
}

func TestBuildSchema_HasBulkMutations(t *testing.T) {
	reg := schemapkg.NewRegistry()
	_, err := reg.Register(petEnvelope())
	require.NoError(t, err)

	mock := newMockService()
	ctr := container.New(container.WithDefaultService(mock))

	schema, err := BuildSchema(reg, ctr)
	require.NoError(t, err)

	mt := schema.MutationType()
	require.NotNil(t, mt, "schema must have a Mutation type")

	fields := mt.Fields()
	assert.Contains(t, fields, "bulkCreatePet", "expected bulkCreatePet mutation")
	assert.Contains(t, fields, "bulkUpdatePet", "expected bulkUpdatePet mutation")
	assert.Contains(t, fields, "bulkDeletePet", "expected bulkDeletePet mutation")
}

func TestBuildSchema_HasSubscriptions(t *testing.T) {
	reg := schemapkg.NewRegistry()
	_, err := reg.Register(petEnvelope())
	require.NoError(t, err)

	mock := newMockService()
	bus := event.NewBus()
	ctr := container.New(
		container.WithDefaultService(mock),
		container.WithEventBus(bus),
	)

	schema, err := BuildSchema(reg, ctr)
	require.NoError(t, err)

	st := schema.SubscriptionType()
	require.NotNil(t, st, "schema must have a Subscription type when an event bus is wired")

	fields := st.Fields()
	assert.Contains(t, fields, "onPetCreated", "expected onPetCreated subscription")
	assert.Contains(t, fields, "onPetUpdated", "expected onPetUpdated subscription")
	assert.Contains(t, fields, "onPetDeleted", "expected onPetDeleted subscription")
}

// ---------------------------------------------------------------------------
// Resolver integration tests via gql.Do
// ---------------------------------------------------------------------------

func TestGraphQL_CreateAndGet(t *testing.T) {
	schema, _ := setupTestSchema(t)

	// Step 1 — create a pet. Pass input as a variable so the JSON scalar
	// arrives at the resolver already coerced to map[string]any.
	createResult := doQueryWithVars(schema,
		`mutation CreatePet($input: JSON!) { createPet(input: $input) { entity_id name age } }`,
		map[string]any{"input": map[string]any{"name": "Fido", "age": 3}},
	)
	require.Empty(t, createResult.Errors, "createPet must not return errors")

	data := createResult.Data.(map[string]any)
	created := data["createPet"].(map[string]any)
	entityID, ok := created["entity_id"].(string)
	require.True(t, ok, "entity_id must be a string")
	assert.NotEmpty(t, entityID)
	assert.Equal(t, "Fido", created["name"])
	assert.Equal(t, 3, created["age"])

	// Step 2 — retrieve the same pet by entity ID.
	getResult := doQueryWithVars(schema,
		`query GetPet($id: ID!) { getPet(entityId: $id) { entity_id name age } }`,
		map[string]any{"id": entityID},
	)
	require.Empty(t, getResult.Errors, "getPet must not return errors")

	getData := getResult.Data.(map[string]any)
	got := getData["getPet"].(map[string]any)
	assert.Equal(t, entityID, got["entity_id"])
	assert.Equal(t, "Fido", got["name"])
	assert.Equal(t, 3, got["age"])
}

func TestGraphQL_ListPets(t *testing.T) {
	schema, mock := setupTestSchema(t)
	ctx := context.Background()

	// Pre-populate two pets directly via the mock so list sees exactly 2.
	_, err := mock.Create(ctx, "pet", map[string]any{"name": "Alpha", "age": 1})
	require.NoError(t, err)
	_, err = mock.Create(ctx, "pet", map[string]any{"name": "Beta", "age": 2})
	require.NoError(t, err)

	result := doQuery(schema, `query {
		listPets {
			total
			hasMore
			items {
				entity_id
				name
			}
		}
	}`)
	require.Empty(t, result.Errors, "listPets must not return errors")

	data := result.Data.(map[string]any)
	listData := data["listPets"].(map[string]any)

	total, ok := listData["total"].(int)
	require.True(t, ok, "total must be an int")
	assert.Equal(t, 2, total)

	items, ok := listData["items"].([]any)
	require.True(t, ok, "items must be a slice")
	assert.Len(t, items, 2)
}

func TestGraphQL_UpdatePet(t *testing.T) {
	schema, _ := setupTestSchema(t)

	// Create a pet first.
	createResult := doQueryWithVars(schema,
		`mutation CreatePet($input: JSON!) { createPet(input: $input) { entity_id } }`,
		map[string]any{"input": map[string]any{"name": "OldName", "age": 1}},
	)
	require.Empty(t, createResult.Errors)

	createData := createResult.Data.(map[string]any)
	entityID := createData["createPet"].(map[string]any)["entity_id"].(string)

	// Update the name via variables so the JSON scalar is properly coerced.
	updateResult := doQueryWithVars(schema,
		`mutation UpdatePet($id: ID!, $input: JSON!) {
			updatePet(entityId: $id, input: $input) { entity_id name age record_version }
		}`,
		map[string]any{
			"id":    entityID,
			"input": map[string]any{"name": "NewName", "age": 5},
		},
	)
	require.Empty(t, updateResult.Errors, "updatePet must not return errors")

	updateData := updateResult.Data.(map[string]any)
	updated := updateData["updatePet"].(map[string]any)
	assert.Equal(t, entityID, updated["entity_id"])
	assert.Equal(t, "NewName", updated["name"])
	assert.Equal(t, 5, updated["age"])
	assert.Equal(t, 2, updated["record_version"], "record version must be incremented")
}

func TestGraphQL_DeletePet(t *testing.T) {
	schema, _ := setupTestSchema(t)

	// Create a pet to delete.
	createResult := doQueryWithVars(schema,
		`mutation CreatePet($input: JSON!) { createPet(input: $input) { entity_id } }`,
		map[string]any{"input": map[string]any{"name": "ToDelete"}},
	)
	require.Empty(t, createResult.Errors)

	createData := createResult.Data.(map[string]any)
	entityID := createData["createPet"].(map[string]any)["entity_id"].(string)

	// Delete the pet.
	deleteResult := doQueryWithVars(schema,
		`mutation DeletePet($id: ID!) { deletePet(entityId: $id) { deleted entityId } }`,
		map[string]any{"id": entityID},
	)
	require.Empty(t, deleteResult.Errors, "deletePet must not return errors")

	deleteData := deleteResult.Data.(map[string]any)
	dr := deleteData["deletePet"].(map[string]any)
	assert.Equal(t, true, dr["deleted"])
	assert.Equal(t, entityID, dr["entityId"])
}

func TestGraphQL_BulkCreate(t *testing.T) {
	schema, _ := setupTestSchema(t)

	result := doQueryWithVars(schema,
		`mutation BulkCreate($inputs: [JSON!]!) {
			bulkCreatePet(inputs: $inputs) { succeeded failed items { entity_id name } }
		}`,
		map[string]any{
			"inputs": []any{
				map[string]any{"name": "Pet1", "age": 1},
				map[string]any{"name": "Pet2", "age": 2},
				map[string]any{"name": "Pet3", "age": 3},
			},
		},
	)
	require.Empty(t, result.Errors, "bulkCreatePet must not return errors")

	data := result.Data.(map[string]any)
	bulk := data["bulkCreatePet"].(map[string]any)

	assert.Equal(t, 3, bulk["succeeded"], "succeeded must be 3")
	assert.Equal(t, 0, bulk["failed"], "failed must be 0")

	items, ok := bulk["items"].([]any)
	require.True(t, ok, "items must be a slice")
	assert.Len(t, items, 3)
}

func TestGraphQL_BulkUpdate(t *testing.T) {
	schema, mock := setupTestSchema(t)
	ctx := context.Background()

	// Pre-create two pets.
	doc1, err := mock.Create(ctx, "pet", map[string]any{"name": "Pet1", "age": 1})
	require.NoError(t, err)
	doc2, err := mock.Create(ctx, "pet", map[string]any{"name": "Pet2", "age": 2})
	require.NoError(t, err)

	result := doQueryWithVars(schema,
		`mutation BulkUpdate($items: [JSON!]!) {
			bulkUpdatePet(items: $items) { succeeded failed items { entity_id name age } }
		}`,
		map[string]any{
			"items": []any{
				map[string]any{"entity_id": doc1.EntityID, "data": map[string]any{"name": "Updated1", "age": 10}},
				map[string]any{"entity_id": doc2.EntityID, "data": map[string]any{"name": "Updated2", "age": 20}},
			},
		},
	)
	require.Empty(t, result.Errors, "bulkUpdatePet must not return errors")

	data := result.Data.(map[string]any)
	bulk := data["bulkUpdatePet"].(map[string]any)

	assert.Equal(t, 2, bulk["succeeded"], "succeeded must be 2")
	assert.Equal(t, 0, bulk["failed"], "failed must be 0")

	items, ok := bulk["items"].([]any)
	require.True(t, ok, "items must be a slice")
	assert.Len(t, items, 2)

	// Verify updated names appear in the response.
	names := make(map[string]bool, 2)
	for _, raw := range items {
		item := raw.(map[string]any)
		if name, ok := item["name"].(string); ok {
			names[name] = true
		}
	}
	assert.True(t, names["Updated1"], "Updated1 should be in bulk update response")
	assert.True(t, names["Updated2"], "Updated2 should be in bulk update response")
}

func TestGraphQL_BulkDelete(t *testing.T) {
	schema, mock := setupTestSchema(t)
	ctx := context.Background()

	// Pre-create two pets.
	doc1, err := mock.Create(ctx, "pet", map[string]any{"name": "Pet1"})
	require.NoError(t, err)
	doc2, err := mock.Create(ctx, "pet", map[string]any{"name": "Pet2"})
	require.NoError(t, err)

	result := doQuery(schema, fmt.Sprintf(`mutation {
		bulkDeletePet(ids: [%q, %q]) {
			succeeded
			failed
		}
	}`, doc1.EntityID, doc2.EntityID))
	require.Empty(t, result.Errors, "bulkDeletePet must not return errors")

	data := result.Data.(map[string]any)
	bulk := data["bulkDeletePet"].(map[string]any)

	assert.Equal(t, 2, bulk["succeeded"], "succeeded must be 2")
	assert.Equal(t, 0, bulk["failed"], "failed must be 0")

	// Verify the documents are gone.
	_, findErr := mock.FindByID(ctx, "pet", doc1.EntityID)
	assert.Error(t, findErr, "deleted pet1 must not be findable")
	_, findErr = mock.FindByID(ctx, "pet", doc2.EntityID)
	assert.Error(t, findErr, "deleted pet2 must not be findable")
}

// ---------------------------------------------------------------------------
// Edge-case resolver tests
// ---------------------------------------------------------------------------

func TestGraphQL_GetPet_NotFound(t *testing.T) {
	schema, _ := setupTestSchema(t)

	result := doQuery(schema, `query {
		getPet(entityId: "does-not-exist") {
			entity_id
		}
	}`)

	// graphql-go surfaces resolver errors in the Errors slice and sets the
	// field to null in Data, so we only check that an error was reported.
	assert.NotEmpty(t, result.Errors, "getPet for missing entity must return an error")
}

func TestGraphQL_CreatePet_AuditFieldsPresent(t *testing.T) {
	schema, _ := setupTestSchema(t)

	result := doQueryWithVars(schema,
		`mutation CreatePet($input: JSON!) {
			createPet(input: $input) { id entity_id record_version schema_version etag }
		}`,
		map[string]any{"input": map[string]any{"name": "AuditTest", "age": 7}},
	)
	require.Empty(t, result.Errors)

	data := result.Data.(map[string]any)
	doc := data["createPet"].(map[string]any)

	assert.NotEmpty(t, doc["id"], "id must be present")
	assert.NotEmpty(t, doc["entity_id"], "entity_id must be present")
	assert.Equal(t, 1, doc["record_version"], "initial record_version must be 1")
	assert.Equal(t, "1.0.0", doc["schema_version"])
	assert.NotEmpty(t, doc["etag"], "etag must be present")
}

func TestGraphQL_UpdatePet_NotFound(t *testing.T) {
	schema, _ := setupTestSchema(t)

	result := doQueryWithVars(schema,
		`mutation UpdatePet($id: ID!, $input: JSON!) {
			updatePet(entityId: $id, input: $input) { entity_id }
		}`,
		map[string]any{"id": "ghost", "input": map[string]any{"name": "Ghost"}},
	)
	assert.NotEmpty(t, result.Errors, "updatePet for missing entity must return an error")
}

func TestGraphQL_DeletePet_NotFound(t *testing.T) {
	schema, _ := setupTestSchema(t)

	result := doQuery(schema, `mutation {
		deletePet(entityId: "ghost") {
			deleted
		}
	}`)
	assert.NotEmpty(t, result.Errors, "deletePet for missing entity must return an error")
}

// ---------------------------------------------------------------------------
// BuildVersionedSchema tests
// ---------------------------------------------------------------------------

// versionedSchemaSetup builds a registry with two versions of "pet" plus
// one version of "toy", then constructs a BuildVersionedSchema-backed
// gql.Schema. It returns the schema, a map of mock services keyed by schema
// name, and the registry.
func versionedSchemaSetup(t *testing.T) (gql.Schema, map[string]*mockService) {
	t.Helper()

	reg := schemapkg.NewRegistry()

	// pet v1 — minimal fields.
	_, err := reg.Register(&schemapkg.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
		Active: true,
	})
	require.NoError(t, err)

	// pet v2 — latest; adds microchip_id.
	_, err = reg.Register(&schemapkg.SchemaEnvelope{
		Name:    "pet",
		Version: "2.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"microchip_id": map[string]any{"type": "string"},
			},
		},
		Active: true,
	})
	require.NoError(t, err)

	// toy v1 — single version schema.
	_, err = reg.Register(&schemapkg.SchemaEnvelope{
		Name:    "toy",
		Version: "1.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			},
		},
		Active: true,
	})
	require.NoError(t, err)

	mocks := map[string]*mockService{
		"pet": newMockService(),
		"toy": newMockService(),
	}

	// Route each schema name to its mock via the container default service.
	// The container's ResolveService falls back to the default for any name,
	// so we need a dispatcher that picks the right mock.
	dispatcher := &dispatchService{mocks: mocks}
	ctr := container.New(container.WithDefaultService(dispatcher))

	schema, err := BuildVersionedSchema(reg, ctr)
	require.NoError(t, err)
	return schema, mocks
}

// dispatchService routes service calls to the correct mockService by
// inspecting the schemaName argument. This lets a single container serve
// multiple schemas in tests.
type dispatchService struct {
	mocks map[string]*mockService
}

func (d *dispatchService) mock(name string) *mockService {
	if m, ok := d.mocks[name]; ok {
		return m
	}
	// Fallback to the first available mock to keep the resolver happy.
	for _, m := range d.mocks {
		return m
	}
	return newMockService()
}

func (d *dispatchService) Create(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error) {
	return d.mock(schemaName).Create(ctx, schemaName, input)
}
func (d *dispatchService) FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error) {
	return d.mock(schemaName).FindByID(ctx, schemaName, entityID)
}
func (d *dispatchService) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	return d.mock(schemaName).List(ctx, schemaName, q)
}
func (d *dispatchService) Update(ctx context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error) {
	return d.mock(schemaName).Update(ctx, schemaName, entityID, input)
}
func (d *dispatchService) Delete(ctx context.Context, schemaName string, entityID string) error {
	return d.mock(schemaName).Delete(ctx, schemaName, entityID)
}
func (d *dispatchService) BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error) {
	return d.mock(schemaName).BulkCreate(ctx, schemaName, inputs)
}
func (d *dispatchService) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	return d.mock(schemaName).BulkUpdate(ctx, schemaName, items)
}
func (d *dispatchService) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	return d.mock(schemaName).BulkDelete(ctx, schemaName, ids)
}

func TestBuildVersionedSchema_HasQueryType(t *testing.T) {
	schema, _ := versionedSchemaSetup(t)
	assert.NotNil(t, schema.QueryType(), "BuildVersionedSchema must produce a Query root type")
}

func TestBuildVersionedSchema_HasMutationType(t *testing.T) {
	schema, _ := versionedSchemaSetup(t)
	assert.NotNil(t, schema.MutationType(), "BuildVersionedSchema must produce a Mutation root type")
}

func TestBuildVersionedSchema_VersionedQueryFieldsPresent(t *testing.T) {
	schema, _ := versionedSchemaSetup(t)
	qFields := schema.QueryType().Fields()

	// versioned fields for pet v1
	assert.Contains(t, qFields, "getPetV1_0_0", "versioned get field for pet v1.0.0 must exist")
	assert.Contains(t, qFields, "listPetV1_0_0s", "versioned list field for pet v1.0.0 must exist")

	// versioned fields for pet v2 (latest)
	assert.Contains(t, qFields, "getPetV2_0_0", "versioned get field for pet v2.0.0 must exist")
	assert.Contains(t, qFields, "listPetV2_0_0s", "versioned list field for pet v2.0.0 must exist")

	// unversioned aliases for latest (pet v2)
	assert.Contains(t, qFields, "getPet", "unversioned get alias for latest pet must exist")
	assert.Contains(t, qFields, "listPets", "unversioned list alias for latest pet must exist")

	// versioned fields for toy v1 (which is also latest)
	assert.Contains(t, qFields, "getToyV1_0_0", "versioned get field for toy v1.0.0 must exist")
	assert.Contains(t, qFields, "getToy", "unversioned get alias for latest toy must exist")
}

func TestBuildVersionedSchema_VersionedMutationFieldsPresent(t *testing.T) {
	schema, _ := versionedSchemaSetup(t)
	mFields := schema.MutationType().Fields()

	assert.Contains(t, mFields, "createPetV1_0_0")
	assert.Contains(t, mFields, "updatePetV1_0_0")
	assert.Contains(t, mFields, "deletePetV1_0_0")
	assert.Contains(t, mFields, "createPetV2_0_0")
	assert.Contains(t, mFields, "updatePetV2_0_0")
	assert.Contains(t, mFields, "deletePetV2_0_0")

	// Unversioned aliases for latest.
	assert.Contains(t, mFields, "createPet")
	assert.Contains(t, mFields, "updatePet")
	assert.Contains(t, mFields, "deletePet")
}

func TestBuildVersionedSchema_LatestTypeUsesUnversionedName(t *testing.T) {
	schema, _ := versionedSchemaSetup(t)
	typeMap := schema.TypeMap()

	// latest pet version must appear as "Pet" (not "PetV2_0_0").
	_, hasPet := typeMap["Pet"]
	assert.True(t, hasPet, "latest pet type must be registered as 'Pet'")

	// older pet version must appear as "PetV1_0_0".
	_, hasPetV1 := typeMap["PetV1_0_0"]
	assert.True(t, hasPetV1, "older pet version must be registered as 'PetV1_0_0'")

	// no stray versioned type for the latest should exist.
	_, hasPetV2 := typeMap["PetV2_0_0"]
	assert.False(t, hasPetV2, "latest pet version must NOT also appear as 'PetV2_0_0'")
}

func TestBuildVersionedSchema_EmptyRegistry(t *testing.T) {
	reg := schemapkg.NewRegistry()
	ctr := container.New()

	schema, err := BuildVersionedSchema(reg, ctr)
	require.NoError(t, err)
	assert.NotNil(t, schema.QueryType(), "empty registry must still produce a valid placeholder schema")
}

func TestBuildVersionedSchema_CreateViaVersionedField(t *testing.T) {
	schema, mocks := versionedSchemaSetup(t)

	// Create a pet via the versioned v2 mutation field.
	result := doQueryWithVars(schema, `
		mutation CreatePet($input: JSON!) {
			createPetV2_0_0(input: $input) {
				entity_id
				record_version
			}
		}
	`, map[string]any{
		"input": map[string]any{"name": "Rex", "microchip_id": "MC123"},
	})

	require.Empty(t, result.Errors, "createPetV2_0_0 must succeed without errors")
	data, ok := result.Data.(map[string]any)
	require.True(t, ok)
	created, ok := data["createPetV2_0_0"].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, created["entity_id"])
	assert.Equal(t, 1, created["record_version"])

	// Confirm the mock stored the document.
	assert.Len(t, mocks["pet"].docs, 1)
}

func TestBuildVersionedSchema_GetViaUnversionedAlias(t *testing.T) {
	schema, mocks := versionedSchemaSetup(t)

	// Pre-seed a pet document directly in the mock.
	ctx := context.Background()
	doc, err := mocks["pet"].Create(ctx, "pet", map[string]any{"name": "Whiskers"})
	require.NoError(t, err)

	result := doQueryWithVars(schema, `
		query GetPet($id: ID!) {
			getPet(entityId: $id) {
				entity_id
				record_version
			}
		}
	`, map[string]any{"id": doc.EntityID})

	require.Empty(t, result.Errors, "getPet (unversioned alias) must resolve the document")
	data := result.Data.(map[string]any)
	got := data["getPet"].(map[string]any)
	assert.Equal(t, doc.EntityID, got["entity_id"])
}

func TestVersionSuffix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1.0.0", "V1_0_0"},
		{"2.3.1", "V2_3_1"},
		{"v1.0.0", "V1_0_0"}, // leading "v" is stripped
		{"10.0.0", "V10_0_0"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, versionSuffix(tc.input))
		})
	}
}
