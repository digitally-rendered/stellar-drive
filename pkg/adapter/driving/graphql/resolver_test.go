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
