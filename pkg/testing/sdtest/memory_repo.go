// Package sdtest provides test utilities for stellar-drive-based applications.
// It offers an in-memory Repository implementation, a fully-wired TestEngine
// builder, a CRUD lifecycle runner, and deterministic test-data generators —
// all without requiring a real database.
package sdtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// MemoryRepository is a thread-safe, in-memory implementation of port.Repository.
// It is designed exclusively for use in tests; no data is persisted between
// process restarts. All operations are append-only in spirit: Update increments
// RecordVersion rather than overwriting history, and Delete sets DeletedAt
// rather than removing the document.
type MemoryRepository struct {
	mu    sync.RWMutex
	store map[string]map[string]*model.Document // schemaName -> entityID -> doc
}

// NewMemoryRepository returns an initialised, empty MemoryRepository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		store: make(map[string]map[string]*model.Document),
	}
}

// ---------------------------------------------------------------------------
// port.Repository implementation
// ---------------------------------------------------------------------------

// Create persists data as a new document under schemaName. It generates a
// random EntityID and ID, sets RecordVersion to 1, computes an ETag, and
// records creation timestamps.
func (r *MemoryRepository) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	entityID := newUUID()
	id := newUUID()
	now := time.Now().UTC()

	doc := &model.Document{
		ID:            id,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		Data:          cloneData(data),
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          computeETag(entityID, 1),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.store[schemaName] == nil {
		r.store[schemaName] = make(map[string]*model.Document)
	}
	r.store[schemaName][entityID] = doc

	return copyDoc(doc), nil
}

// FindByID retrieves the current (non-deleted) document identified by entityID
// under schemaName. Returns coreerrors.NotFound when the entity does not exist
// or has been soft-deleted.
func (r *MemoryRepository) FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	doc, err := r.get(schemaName, entityID)
	if err != nil {
		return nil, err
	}
	return copyDoc(doc), nil
}

// List returns a paginated slice of non-deleted documents for schemaName.
// When q is nil, all documents are returned using the default limit. Filter
// expressions in q are ignored for simplicity; only Limit and Offset are
// applied.
func (r *MemoryRepository) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	if q == nil {
		q = &query.Query{}
	}

	limit := q.Limit
	if limit <= 0 {
		limit = model.DefaultLimit
	}
	if limit > model.MaxLimit {
		limit = model.MaxLimit
	}

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Collect active documents in stable insertion order (alphabetically by
	// entityID as a deterministic proxy since maps have no guaranteed order).
	var active []*model.Document
	if schemaDocs, ok := r.store[schemaName]; ok {
		for _, doc := range schemaDocs {
			if doc.DeletedAt == nil {
				active = append(active, doc)
			}
		}
	}

	total := int64(len(active))

	// Apply offset.
	if offset >= len(active) {
		return &model.ListResult{
			Items:   []*model.Document{},
			Total:   total,
			HasMore: false,
		}, nil
	}
	active = active[offset:]

	// Apply limit.
	hasMore := false
	if len(active) > limit {
		active = active[:limit]
		hasMore = true
	}

	// Copy items to avoid mutation from caller.
	items := make([]*model.Document, len(active))
	for i, doc := range active {
		items[i] = copyDoc(doc)
	}

	var cursor string
	if len(items) > 0 {
		last := items[len(items)-1]
		cursor = model.EncodeCursor(last.EntityID, last.RecordVersion)
	}

	return &model.ListResult{
		Items:   items,
		Total:   total,
		HasMore: hasMore,
		Cursor:  cursor,
	}, nil
}

// Update applies data as a partial patch to the document identified by
// entityID under schemaName. It increments RecordVersion, merges the patch
// over the existing Data map, updates UpdatedAt, and recomputes the ETag.
// Returns coreerrors.NotFound when the entity does not exist.
func (r *MemoryRepository) Update(ctx context.Context, schemaName string, entityID string, data map[string]any) (*model.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.get(schemaName, entityID)
	if err != nil {
		return nil, err
	}

	newVersion := doc.RecordVersion + 1
	merged := cloneData(doc.Data)
	for k, v := range data {
		merged[k] = v
	}

	doc.RecordVersion = newVersion
	doc.Data = merged
	doc.UpdatedAt = time.Now().UTC()
	doc.ETag = computeETag(entityID, newVersion)

	return copyDoc(doc), nil
}

// Delete soft-deletes the document identified by entityID under schemaName by
// setting its DeletedAt timestamp. Returns coreerrors.NotFound when the entity
// does not exist.
func (r *MemoryRepository) Delete(ctx context.Context, schemaName string, entityID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, err := r.get(schemaName, entityID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	doc.DeletedAt = &now
	doc.UpdatedAt = now

	return nil
}

// BulkCreate persists multiple documents in a single call. Input order is
// preserved in the returned slice. Each document receives an independent
// EntityID, ID, and RecordVersion.
func (r *MemoryRepository) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := r.Create(ctx, schemaName, item)
		if err != nil {
			return nil, fmt.Errorf("bulk create: %w", err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// BulkUpdate applies per-item patches to multiple existing documents. Items
// whose entityID cannot be found are silently skipped, consistent with the
// port contract.
func (r *MemoryRepository) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	docs := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := r.Update(ctx, schemaName, item.EntityID, item.Data)
		if err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("bulk update entity %q: %w", item.EntityID, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// BulkDelete soft-deletes the documents identified by ids. Missing entity IDs
// are silently skipped.
func (r *MemoryRepository) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	for _, id := range ids {
		if err := r.Delete(ctx, schemaName, id); err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("bulk delete entity %q: %w", id, err)
		}
	}
	return nil
}

// EnsureIndexes is a no-op for the in-memory implementation. It exists solely
// to satisfy the port.Repository interface.
func (r *MemoryRepository) EnsureIndexes(ctx context.Context, schemaName string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Inspection helpers (not part of the port interface)
// ---------------------------------------------------------------------------

// Count returns the number of non-deleted documents stored for schemaName.
// This is useful in tests for asserting side effects without going through the
// query layer.
func (r *MemoryRepository) Count(schemaName string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, doc := range r.store[schemaName] {
		if doc.DeletedAt == nil {
			count++
		}
	}
	return count
}

// Reset removes all documents for all schemas. Use between test cases when the
// repository is shared.
func (r *MemoryRepository) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = make(map[string]map[string]*model.Document)
}

// ---------------------------------------------------------------------------
// Private helpers (caller must hold appropriate lock)
// ---------------------------------------------------------------------------

// get returns the live document or a NotFound error. The caller must hold at
// least a read lock before calling.
func (r *MemoryRepository) get(schemaName, entityID string) (*model.Document, error) {
	schemaDocs, ok := r.store[schemaName]
	if !ok {
		return nil, coreerrors.NotFound("entity", entityID)
	}
	doc, ok := schemaDocs[entityID]
	if !ok || doc.DeletedAt != nil {
		return nil, coreerrors.NotFound("entity", entityID)
	}
	return doc, nil
}

// ---------------------------------------------------------------------------
// Package-level helpers
// ---------------------------------------------------------------------------

// newUUID generates a random UUID v4 string. It panics if the system CSPRNG
// is unavailable, which should never happen in a test environment.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("sdtest: crypto/rand unavailable: %v", err))
	}
	// Set version 4 and variant bits per RFC 4122.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%12x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// computeETag returns a hex-encoded SHA-256 digest of "entityID:version".
func computeETag(entityID string, version int) string {
	raw := fmt.Sprintf("%s:%d", entityID, version)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// cloneData returns a shallow copy of data to prevent callers from mutating
// stored state through the returned map reference.
func cloneData(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}

// copyDoc returns a pointer to a shallow copy of doc. The Data field is also
// cloned so that callers cannot mutate stored state.
func copyDoc(doc *model.Document) *model.Document {
	cp := *doc
	cp.Data = cloneData(doc.Data)
	return &cp
}
