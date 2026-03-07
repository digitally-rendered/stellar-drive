package sdtest

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// ctx is a shorthand used throughout the tests.
var ctx = context.Background()

// newRepo creates a fresh MemoryRepository for each test.
func newRepo() *MemoryRepository { return NewMemoryRepository() }

// ---------------------------------------------------------------------------
// Create + FindByID
// ---------------------------------------------------------------------------

func TestMemoryRepository_CreateAndFindByID(t *testing.T) {
	tests := []struct {
		name       string
		schemaName string
		data       map[string]any
	}{
		{
			name:       "simple document",
			schemaName: "pets",
			data:       map[string]any{"name": "Fido", "status": "available"},
		},
		{
			name:       "empty data",
			schemaName: "orders",
			data:       map[string]any{},
		},
		{
			name:       "nested data",
			schemaName: "users",
			data:       map[string]any{"profile": map[string]any{"email": "a@b.com"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo()

			created, err := repo.Create(ctx, tc.schemaName, tc.data)
			require.NoError(t, err)
			assert.NotEmpty(t, created.EntityID)
			assert.NotEmpty(t, created.ID)
			assert.Equal(t, tc.schemaName, created.SchemaName)
			assert.Equal(t, 1, created.RecordVersion)
			assert.NotEmpty(t, created.ETag)
			assert.False(t, created.CreatedAt.IsZero())
			assert.False(t, created.UpdatedAt.IsZero())
			assert.Nil(t, created.DeletedAt)

			found, err := repo.FindByID(ctx, tc.schemaName, created.EntityID)
			require.NoError(t, err)
			assert.Equal(t, created.EntityID, found.EntityID)
			assert.Equal(t, tc.schemaName, found.SchemaName)
		})
	}
}

func TestMemoryRepository_FindByID_NotFound(t *testing.T) {
	tests := []struct {
		name       string
		schemaName string
		entityID   string
	}{
		{
			name:       "unknown schema",
			schemaName: "ghosts",
			entityID:   "does-not-exist",
		},
		{
			name:       "known schema, missing entity",
			schemaName: "pets",
			entityID:   "nonexistent-id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo()
			if tc.name == "known schema, missing entity" {
				// Seed one document so the schema bucket exists.
				_, err := repo.Create(ctx, tc.schemaName, map[string]any{"name": "Rex"})
				require.NoError(t, err)
			}

			_, err := repo.FindByID(ctx, tc.schemaName, tc.entityID)
			require.Error(t, err)
			assert.True(t, coreerrors.IsNotFound(err), "expected a NotFound domain error, got: %v", err)
		})
	}
}

// ---------------------------------------------------------------------------
// List with pagination
// ---------------------------------------------------------------------------

func TestMemoryRepository_List_Pagination(t *testing.T) {
	const schema = "pets"

	repo := newRepo()
	// Insert 5 documents.
	for i := 0; i < 5; i++ {
		_, err := repo.Create(ctx, schema, map[string]any{"index": i})
		require.NoError(t, err)
	}

	tests := []struct {
		name        string
		q           *query.Query
		wantLen     int
		wantTotal   int64
		wantHasMore bool
	}{
		{
			name:        "nil query returns all with default limit",
			q:           nil,
			wantLen:     5,
			wantTotal:   5,
			wantHasMore: false,
		},
		{
			name:        "limit 2",
			q:           &query.Query{Limit: 2},
			wantLen:     2,
			wantTotal:   5,
			wantHasMore: true,
		},
		{
			name:        "limit 2 offset 4",
			q:           &query.Query{Limit: 2, Offset: 4},
			wantLen:     1,
			wantTotal:   5,
			wantHasMore: false,
		},
		{
			name:        "offset beyond total",
			q:           &query.Query{Limit: 10, Offset: 10},
			wantLen:     0,
			wantTotal:   5,
			wantHasMore: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := repo.List(ctx, schema, tc.q)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTotal, result.Total)
			assert.Len(t, result.Items, tc.wantLen)
			assert.Equal(t, tc.wantHasMore, result.HasMore)
		})
	}
}

func TestMemoryRepository_List_EmptySchema(t *testing.T) {
	repo := newRepo()
	result, err := repo.List(ctx, "empty-schema", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.Total)
	assert.Empty(t, result.Items)
	assert.False(t, result.HasMore)
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestMemoryRepository_Update_IncrementsVersion(t *testing.T) {
	repo := newRepo()

	created, err := repo.Create(ctx, "pets", map[string]any{"name": "Fido"})
	require.NoError(t, err)
	assert.Equal(t, 1, created.RecordVersion)
	originalETag := created.ETag

	updated, err := repo.Update(ctx, "pets", created.EntityID, map[string]any{"status": "sold"})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.RecordVersion)
	assert.NotEqual(t, originalETag, updated.ETag, "ETag must change after update")
	// Original field is preserved (merge semantics).
	assert.Equal(t, "Fido", updated.Data["name"])
	assert.Equal(t, "sold", updated.Data["status"])
}

func TestMemoryRepository_Update_NotFound(t *testing.T) {
	repo := newRepo()
	_, err := repo.Update(ctx, "pets", "ghost-id", map[string]any{"x": 1})
	require.Error(t, err)
	assert.True(t, coreerrors.IsNotFound(err))
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestMemoryRepository_Delete_SetsTombstone(t *testing.T) {
	repo := newRepo()

	created, err := repo.Create(ctx, "pets", map[string]any{"name": "Fido"})
	require.NoError(t, err)

	err = repo.Delete(ctx, "pets", created.EntityID)
	require.NoError(t, err)

	_, err = repo.FindByID(ctx, "pets", created.EntityID)
	require.Error(t, err)
	assert.True(t, coreerrors.IsNotFound(err), "deleted entity must return NotFound, got: %v", err)

	// The document count should reflect the deletion.
	assert.Equal(t, 0, repo.Count("pets"))
}

func TestMemoryRepository_Delete_NotFound(t *testing.T) {
	repo := newRepo()
	err := repo.Delete(ctx, "pets", "nonexistent")
	require.Error(t, err)
	assert.True(t, coreerrors.IsNotFound(err))
}

// ---------------------------------------------------------------------------
// BulkCreate
// ---------------------------------------------------------------------------

func TestMemoryRepository_BulkCreate(t *testing.T) {
	repo := newRepo()

	items := []map[string]any{
		{"name": "Fido"},
		{"name": "Rex"},
		{"name": "Buddy"},
	}

	docs, err := repo.BulkCreate(ctx, "pets", items)
	require.NoError(t, err)
	require.Len(t, docs, 3)

	// Each document must have a unique EntityID.
	seen := make(map[string]bool)
	for _, doc := range docs {
		assert.NotEmpty(t, doc.EntityID)
		assert.False(t, seen[doc.EntityID], "duplicate EntityID: %s", doc.EntityID)
		seen[doc.EntityID] = true
		assert.Equal(t, 1, doc.RecordVersion)
	}

	assert.Equal(t, 3, repo.Count("pets"))
}

func TestMemoryRepository_BulkCreate_EmptySlice(t *testing.T) {
	repo := newRepo()
	docs, err := repo.BulkCreate(ctx, "pets", []map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, docs)
	assert.Equal(t, 0, repo.Count("pets"))
}

// ---------------------------------------------------------------------------
// BulkUpdate
// ---------------------------------------------------------------------------

func TestMemoryRepository_BulkUpdate(t *testing.T) {
	repo := newRepo()

	docs, err := repo.BulkCreate(ctx, "pets", []map[string]any{
		{"name": "Fido"},
		{"name": "Rex"},
	})
	require.NoError(t, err)
	require.Len(t, docs, 2)

	items := []model.BulkUpdateItem{
		{EntityID: docs[0].EntityID, Data: map[string]any{"status": "sold"}},
		{EntityID: docs[1].EntityID, Data: map[string]any{"status": "available"}},
	}

	updated, err := repo.BulkUpdate(ctx, "pets", items)
	require.NoError(t, err)
	require.Len(t, updated, 2)

	for _, doc := range updated {
		assert.Equal(t, 2, doc.RecordVersion)
	}
}

func TestMemoryRepository_BulkUpdate_SkipsMissing(t *testing.T) {
	repo := newRepo()

	created, err := repo.Create(ctx, "pets", map[string]any{"name": "Fido"})
	require.NoError(t, err)

	items := []model.BulkUpdateItem{
		{EntityID: created.EntityID, Data: map[string]any{"status": "sold"}},
		{EntityID: "ghost-id", Data: map[string]any{"status": "available"}}, // does not exist
	}

	updated, err := repo.BulkUpdate(ctx, "pets", items)
	require.NoError(t, err)
	// Only the existing entity is returned; the missing one is silently skipped.
	assert.Len(t, updated, 1)
	assert.Equal(t, created.EntityID, updated[0].EntityID)
}

// ---------------------------------------------------------------------------
// BulkDelete
// ---------------------------------------------------------------------------

func TestMemoryRepository_BulkDelete(t *testing.T) {
	repo := newRepo()

	docs, err := repo.BulkCreate(ctx, "pets", []map[string]any{
		{"name": "Fido"},
		{"name": "Rex"},
		{"name": "Buddy"},
	})
	require.NoError(t, err)

	ids := []string{docs[0].EntityID, docs[2].EntityID}
	err = repo.BulkDelete(ctx, "pets", ids)
	require.NoError(t, err)

	assert.Equal(t, 1, repo.Count("pets"))

	_, err = repo.FindByID(ctx, "pets", docs[0].EntityID)
	assert.True(t, coreerrors.IsNotFound(err))

	found, err := repo.FindByID(ctx, "pets", docs[1].EntityID)
	require.NoError(t, err)
	assert.Equal(t, "Rex", found.Data["name"])
}

func TestMemoryRepository_BulkDelete_SkipsMissing(t *testing.T) {
	repo := newRepo()

	created, err := repo.Create(ctx, "pets", map[string]any{"name": "Fido"})
	require.NoError(t, err)

	// Mix of existing and non-existing IDs.
	err = repo.BulkDelete(ctx, "pets", []string{created.EntityID, "ghost-1", "ghost-2"})
	require.NoError(t, err)

	assert.Equal(t, 0, repo.Count("pets"))
}

// ---------------------------------------------------------------------------
// EnsureIndexes (no-op)
// ---------------------------------------------------------------------------

func TestMemoryRepository_EnsureIndexes_NoError(t *testing.T) {
	repo := newRepo()
	err := repo.EnsureIndexes(ctx, "any-schema")
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Concurrent access safety
// ---------------------------------------------------------------------------

func TestMemoryRepository_ConcurrentAccess(t *testing.T) {
	repo := newRepo()

	const goroutines = 20
	const docsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < docsPerGoroutine; j++ {
				doc, err := repo.Create(ctx, "concurrent", map[string]any{"x": j})
				if err != nil {
					// Cannot call t.Fatal from a goroutine; panic with a clear message.
					panic("concurrent Create failed: " + err.Error())
				}
				_, err = repo.FindByID(ctx, "concurrent", doc.EntityID)
				if err != nil {
					panic("concurrent FindByID failed: " + err.Error())
				}
				_, err = repo.Update(ctx, "concurrent", doc.EntityID, map[string]any{"y": j})
				if err != nil {
					panic("concurrent Update failed: " + err.Error())
				}
			}
		}()
	}

	wg.Wait()

	// All goroutines created docsPerGoroutine documents each; none were deleted.
	assert.Equal(t, goroutines*docsPerGoroutine, repo.Count("concurrent"))
}

func TestMemoryRepository_ConcurrentListAndWrite(t *testing.T) {
	repo := newRepo()

	// Seed some initial data.
	for i := 0; i < 5; i++ {
		_, err := repo.Create(ctx, "shared", map[string]any{"i": i})
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = repo.Create(ctx, "shared", map[string]any{"writer": i})
		}
	}()

	// Reader goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = repo.List(ctx, "shared", &query.Query{Limit: 10})
		}
	}()

	wg.Wait()
	// No assertion on exact count — we only care that there were no data races.
	assert.Greater(t, repo.Count("shared"), 0)
}

// ---------------------------------------------------------------------------
// Reset helper
// ---------------------------------------------------------------------------

func TestMemoryRepository_Reset_ClearsAll(t *testing.T) {
	repo := newRepo()

	_, err := repo.Create(ctx, "pets", map[string]any{"name": "Fido"})
	require.NoError(t, err)
	_, err = repo.Create(ctx, "orders", map[string]any{"total": 99})
	require.NoError(t, err)

	repo.Reset()

	assert.Equal(t, 0, repo.Count("pets"))
	assert.Equal(t, 0, repo.Count("orders"))
}

// ---------------------------------------------------------------------------
// List cursor encoding
// ---------------------------------------------------------------------------

func TestMemoryRepository_List_CursorPopulated(t *testing.T) {
	repo := newRepo()

	for i := 0; i < 3; i++ {
		_, err := repo.Create(ctx, "items", map[string]any{"i": i})
		require.NoError(t, err)
	}

	result, err := repo.List(ctx, "items", &query.Query{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
	assert.True(t, result.HasMore)
	assert.NotEmpty(t, result.Cursor, "cursor must be non-empty when HasMore is true")
}
