// Package graphql_test contains performance benchmarks for the GraphQL
// driving adapter. Each benchmark measures a distinct phase of the GraphQL
// request lifecycle so that regressions can be isolated to schema
// construction, type generation, or query execution.
//
// Run all benchmarks:
//
//	go test ./benchmarks/graphql/ -bench=. -benchmem
//
// Run a single benchmark:
//
//	go test ./benchmarks/graphql/ -bench=BenchmarkBuildSchema -benchmem
package graphql_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	gql "github.com/graphql-go/graphql"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/graphql"
	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	schemapkg "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// mockService — zero-allocation in-memory port.Service for benchmarks.
// ---------------------------------------------------------------------------

// benchmarkService is a minimal port.Service implementation that returns
// pre-built documents without touching any storage layer. Its methods are
// intentionally thin so that benchmark time reflects the GraphQL machinery
// rather than service overhead.
type benchmarkService struct {
	mu      sync.Mutex
	counter int
}

func (s *benchmarkService) nextID() string {
	s.mu.Lock()
	s.counter++
	id := fmt.Sprintf("entity-%d", s.counter)
	s.mu.Unlock()
	return id
}

func (s *benchmarkService) Create(_ context.Context, schemaName string, input map[string]any) (*model.Document, error) {
	now := time.Now().UTC()
	entityID := s.nextID()
	return &model.Document{
		ID:            "id-" + entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          input,
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-" + entityID,
	}, nil
}

func (s *benchmarkService) FindByID(_ context.Context, schemaName string, entityID string) (*model.Document, error) {
	now := time.Now().UTC()
	return &model.Document{
		ID:            "id-" + entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "Fido", "status": "available"},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-" + entityID,
	}, nil
}

func (s *benchmarkService) List(_ context.Context, schemaName string, _ *query.Query) (*model.ListResult, error) {
	now := time.Now().UTC()
	doc := &model.Document{
		ID:            "id-entity-1",
		EntityID:      "entity-1",
		SchemaName:    schemaName,
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "Fido", "status": "available"},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-entity-1",
	}
	return &model.ListResult{
		Items:   []*model.Document{doc},
		Total:   1,
		HasMore: false,
	}, nil
}

func (s *benchmarkService) Update(_ context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error) {
	now := time.Now().UTC()
	return &model.Document{
		ID:            "id-" + entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 2,
		SchemaVersion: "1.0.0",
		Data:          input,
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-" + entityID + "-v2",
	}, nil
}

func (s *benchmarkService) Delete(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *benchmarkService) BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(inputs))
	for _, input := range inputs {
		doc, err := s.Create(ctx, schemaName, input)
		if err != nil {
			return docs, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (s *benchmarkService) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := s.Update(ctx, schemaName, item.EntityID, item.Data)
		if err != nil {
			return docs, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (s *benchmarkService) BulkDelete(_ context.Context, _ string, _ []string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Schema helpers
// ---------------------------------------------------------------------------

// petstoreEnvelopes returns the five canonical petstore schema envelopes used
// across the benchmarks. Each envelope exercises distinct field types so that
// the type-generation path is exercised thoroughly.
func petstoreEnvelopes() []*schemapkg.SchemaEnvelope {
	return []*schemapkg.SchemaEnvelope{
		{
			Name:    "pet",
			Version: "1.0.0",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"name", "status"},
				"properties": map[string]any{
					"name":       map[string]any{"type": "string"},
					"status":     map[string]any{"type": "string", "enum": []any{"available", "pending", "sold"}},
					"birth_date": map[string]any{"type": "string", "format": "date-time"},
					"weight_kg":  map[string]any{"type": "number"},
					"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			Active: true,
		},
		{
			Name:    "order",
			Version: "1.0.0",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"pet_id", "quantity"},
				"properties": map[string]any{
					"pet_id":    map[string]any{"type": "string", "format": "uuid"},
					"quantity":  map[string]any{"type": "integer"},
					"ship_date": map[string]any{"type": "string", "format": "date-time"},
					"status":    map[string]any{"type": "string", "enum": []any{"placed", "approved", "delivered"}},
					"complete":  map[string]any{"type": "boolean"},
				},
			},
			Active: true,
		},
		{
			Name:    "user",
			Version: "1.0.0",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"username", "email"},
				"properties": map[string]any{
					"username":    map[string]any{"type": "string"},
					"email":       map[string]any{"type": "string", "format": "email"},
					"first_name":  map[string]any{"type": "string"},
					"last_name":   map[string]any{"type": "string"},
					"phone":       map[string]any{"type": "string"},
					"user_status": map[string]any{"type": "integer"},
				},
			},
			Active: true,
		},
		{
			Name:    "tag",
			Version: "1.0.0",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			Active: true,
		},
		{
			Name:    "category",
			Version: "1.0.0",
			Schema: map[string]any{
				"type":     "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name":        map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"parent_id":   map[string]any{"type": "string", "format": "uuid"},
					"sort_order":  map[string]any{"type": "integer"},
				},
			},
			Active: true,
		},
	}
}

// buildPetstoreRegistry registers all five petstore schemas and returns a
// ready-to-use registry. It calls b.Fatal if any registration fails so
// benchmark loops stay clean.
func buildPetstoreRegistry(b *testing.B) *schemapkg.Registry {
	b.Helper()
	reg := schemapkg.NewRegistry()
	for _, env := range petstoreEnvelopes() {
		if _, err := reg.Register(env); err != nil {
			b.Fatalf("register schema %q: %v", env.Name, err)
		}
	}
	return reg
}

// buildPetstoreContainer creates a container with the benchmark service wired
// as the default for all schemas.
func buildPetstoreContainer() *container.Container {
	svc := &benchmarkService{}
	return container.New(container.WithDefaultService(svc))
}

// buildPetstoreSchema is a helper that constructs both a registry and a
// container, then calls BuildSchema. It is used in setup phases that must not
// be included in timer measurements.
func buildPetstoreSchema(b *testing.B) gql.Schema {
	b.Helper()
	reg := buildPetstoreRegistry(b)
	ctr := buildPetstoreContainer()
	schema, err := graphql.BuildSchema(reg, ctr)
	if err != nil {
		b.Fatalf("BuildSchema: %v", err)
	}
	return schema
}

// ---------------------------------------------------------------------------
// Benchmarks — schema construction
// ---------------------------------------------------------------------------

// BenchmarkBuildSchema measures the time required to build a complete GraphQL
// schema from a registry containing 5 petstore schemas. This covers field
// type mapping, object type construction, and graphql-go's internal schema
// validation phase.
func BenchmarkBuildSchema(b *testing.B) {
	reg := buildPetstoreRegistry(b)
	ctr := buildPetstoreContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := graphql.BuildSchema(reg, ctr)
		if err != nil {
			b.Fatalf("BuildSchema: %v", err)
		}
	}
}

// BenchmarkBuildSchema_SingleSchema measures BuildSchema with only one schema
// registered. Comparing this against BenchmarkBuildSchema reveals the per-schema
// cost of schema construction.
func BenchmarkBuildSchema_SingleSchema(b *testing.B) {
	reg := schemapkg.NewRegistry()
	env := petstoreEnvelopes()[0] // pet only
	if _, err := reg.Register(env); err != nil {
		b.Fatalf("register: %v", err)
	}
	ctr := buildPetstoreContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := graphql.BuildSchema(reg, ctr)
		if err != nil {
			b.Fatalf("BuildSchema: %v", err)
		}
	}
}

// BenchmarkBuildVersionedSchema measures BuildSchema when every schema has two
// versions registered. The registry always resolves the latest version, so
// this benchmark shows how the multi-version index overhead affects construction
// time without yet requiring a BuildVersionedSchema variant.
func BenchmarkBuildVersionedSchema(b *testing.B) {
	reg := schemapkg.NewRegistry()

	// Register version 1.0.0 for all five schemas.
	for _, env := range petstoreEnvelopes() {
		if _, err := reg.Register(env); err != nil {
			b.Fatalf("register v1 %q: %v", env.Name, err)
		}
	}

	// Register version 2.0.0 for each schema, adding an extra field to
	// simulate a real schema evolution.
	for _, base := range petstoreEnvelopes() {
		v2 := &schemapkg.SchemaEnvelope{
			Name:    base.Name,
			Version: "2.0.0",
			Schema:  copySchema(base.Schema, "updated_flag", map[string]any{"type": "boolean"}),
			Active:  true,
		}
		if _, err := reg.Register(v2); err != nil {
			b.Fatalf("register v2 %q: %v", v2.Name, err)
		}
	}

	ctr := buildPetstoreContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := graphql.BuildSchema(reg, ctr)
		if err != nil {
			b.Fatalf("BuildSchema versioned: %v", err)
		}
	}
}

// copySchema returns a copy of src's schema map with one additional property
// injected under key extraKey. This is used to synthesise a v2 schema without
// modifying the v1 envelope.
func copySchema(src map[string]any, extraKey string, extraVal any) map[string]any {
	// Shallow-copy the top-level schema map.
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}

	// Deep-copy the properties sub-map and inject the extra field.
	if props, ok := src["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props)+1)
		for k, v := range props {
			newProps[k] = v
		}
		newProps[extraKey] = extraVal
		dst["properties"] = newProps
	}

	return dst
}

// ---------------------------------------------------------------------------
// Benchmarks — query execution
// ---------------------------------------------------------------------------

// BenchmarkGraphQLQuery measures the end-to-end latency of a simple getPet
// query resolved against an in-memory schema. The schema and service are built
// once in the setup phase so that benchmark time isolates gql.Do overhead.
func BenchmarkGraphQLQuery(b *testing.B) {
	schema := buildPetstoreSchema(b)

	queryStr := `query { getPet(entityId: "entity-1") { entity_id name status record_version } }`
	params := gql.Params{
		Schema:        schema,
		RequestString: queryStr,
		Context:       context.Background(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := gql.Do(params)
		if len(result.Errors) > 0 {
			b.Fatalf("query errors: %v", result.Errors)
		}
	}
}

// BenchmarkGraphQLQuery_List measures a list query that returns a single-item
// result. This exercises the list resolver and result marshalling path.
func BenchmarkGraphQLQuery_List(b *testing.B) {
	schema := buildPetstoreSchema(b)

	queryStr := `query { listPets { total hasMore items { entity_id name status } } }`
	params := gql.Params{
		Schema:        schema,
		RequestString: queryStr,
		Context:       context.Background(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := gql.Do(params)
		if len(result.Errors) > 0 {
			b.Fatalf("list query errors: %v", result.Errors)
		}
	}
}

// BenchmarkGraphQLMutation measures the latency of a createPet mutation.
// The input is provided via VariableValues so that the JSON scalar coercion
// path is exercised in the same way as a real HTTP client request.
func BenchmarkGraphQLMutation(b *testing.B) {
	schema := buildPetstoreSchema(b)

	mutationStr := `
		mutation CreatePet($input: JSON!) {
			createPet(input: $input) { entity_id name status record_version etag }
		}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := gql.Do(gql.Params{
			Schema:        schema,
			RequestString: mutationStr,
			VariableValues: map[string]any{
				"input": map[string]any{
					"name":   "Benchmark Pet",
					"status": "available",
				},
			},
			Context: context.Background(),
		})
		if len(result.Errors) > 0 {
			b.Fatalf("mutation errors: %v", result.Errors)
		}
	}
}

// BenchmarkGraphQLMutation_Update measures the latency of an updatePet
// mutation, which exercises the update resolver path separately from create.
func BenchmarkGraphQLMutation_Update(b *testing.B) {
	schema := buildPetstoreSchema(b)

	mutationStr := `
		mutation UpdatePet($id: ID!, $input: JSON!) {
			updatePet(entityId: $id, input: $input) { entity_id name status record_version }
		}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := gql.Do(gql.Params{
			Schema:        schema,
			RequestString: mutationStr,
			VariableValues: map[string]any{
				"id": fmt.Sprintf("entity-%d", i+1),
				"input": map[string]any{
					"name":   "Updated Pet",
					"status": "sold",
				},
			},
			Context: context.Background(),
		})
		if len(result.Errors) > 0 {
			b.Fatalf("update mutation errors: %v", result.Errors)
		}
	}
}

// BenchmarkGraphQLParallel measures throughput when multiple goroutines execute
// queries concurrently against the same schema. The schema is immutable after
// construction so no locking is required inside the benchmark.
func BenchmarkGraphQLParallel(b *testing.B) {
	schema := buildPetstoreSchema(b)

	queryStr := `query { getPet(entityId: "entity-1") { entity_id name } }`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		params := gql.Params{
			Schema:        schema,
			RequestString: queryStr,
			Context:       context.Background(),
		}
		for pb.Next() {
			result := gql.Do(params)
			if len(result.Errors) > 0 {
				b.Errorf("parallel query errors: %v", result.Errors)
			}
		}
	})
}

// BenchmarkGraphQLSchemaCount measures BuildSchema with an increasing number
// of registered schemas to characterise the O(n) cost of schema construction.
func BenchmarkGraphQLSchemaCount(b *testing.B) {
	bases := petstoreEnvelopes()

	for _, n := range []int{1, 3, 5} {
		b.Run(fmt.Sprintf("%d_schemas", n), func(b *testing.B) {
			reg := schemapkg.NewRegistry()
			for i := 0; i < n && i < len(bases); i++ {
				if _, err := reg.Register(bases[i]); err != nil {
					b.Fatalf("register: %v", err)
				}
			}
			ctr := buildPetstoreContainer()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := graphql.BuildSchema(reg, ctr)
				if err != nil {
					b.Fatalf("BuildSchema: %v", err)
				}
			}
		})
	}
}
