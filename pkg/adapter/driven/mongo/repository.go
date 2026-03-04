package mongo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitally-rendered/stellar-drive/internal/jsonutil"
	"github.com/digitally-rendered/stellar-drive/internal/stringutil"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoRepository implements port.Repository with append-only versioned
// persistence. Every mutation appends a new document version to the collection;
// no existing document is ever modified or deleted in place.
type MongoRepository struct {
	conn *Connection
}

// NewRepository returns a MongoRepository that uses the given Connection.
func NewRepository(conn *Connection) *MongoRepository {
	return &MongoRepository{conn: conn}
}

// collectionName returns the MongoDB collection name for a given schemaName.
// The schema name is lower-cased and pluralised.
func collectionName(schemaName string) string {
	return stringutil.Pluralize(schemaName)
}

// generateETag produces a SHA-256 hex digest of the JSON-encoded data map.
func generateETag(data map[string]any) string {
	b, _ := json.Marshal(data)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Create persists a new document and returns the stored Document.
func (r *MongoRepository) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	now := time.Now().UTC()
	entityID := uuid.New().String()
	docID := uuid.New().String()

	if data == nil {
		data = map[string]any{}
	}

	doc := &model.Document{
		ID:            docID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		Data:          data,
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          generateETag(data),
	}

	coll := r.conn.Collection(collectionName(schemaName))
	if _, err := coll.InsertOne(ctx, doc); err != nil {
		return nil, coreerrors.Internal("failed to create document", err)
	}

	return doc, nil
}

// FindByID retrieves the current (non-deleted) document for entityID.
// It fetches the highest record_version and returns NotFound when the latest
// version is a tombstone or when no document exists.
func (r *MongoRepository) FindByID(ctx context.Context, schemaName, entityID string) (*model.Document, error) {
	coll := r.conn.Collection(collectionName(schemaName))

	opts := mongooptions.FindOne().SetSort(bson.D{{Key: "record_version", Value: -1}})
	filter := bson.D{{Key: "entity_id", Value: entityID}}

	var doc model.Document
	err := coll.FindOne(ctx, filter, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}
	if err != nil {
		return nil, coreerrors.Internal("failed to find document", err)
	}

	if doc.IsTombstone() {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}

	return &doc, nil
}

// List executes a structured query using an aggregation pipeline that resolves
// the latest version per entity, filters tombstones, applies user filters and
// sort, and returns a paginated ListResult.
func (r *MongoRepository) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	coll := r.conn.Collection(collectionName(schemaName))

	// --- Build aggregation pipeline ---

	// Step 1: sort so $group picks the latest version.
	pipeline := mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{
			{Key: "entity_id", Value: 1},
			{Key: "record_version", Value: -1},
		}}},
		// Step 2: group by entity_id, keep the first (= latest) document.
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$entity_id"},
			{Key: "doc", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		// Step 3: promote the doc field back to the root.
		{{Key: "$replaceRoot", Value: bson.D{{Key: "newRoot", Value: "$doc"}}}},
		// Step 4: filter out tombstones.
		{{Key: "$match", Value: bson.D{{Key: "deleted_at", Value: nil}}}},
	}

	// Step 5: apply user-supplied filter if provided.
	if q != nil && q.Filter != nil {
		filterDoc, err := TranslateFilter(q.Filter)
		if err != nil {
			return nil, coreerrors.BadRequest(fmt.Sprintf("invalid filter: %s", err))
		}
		if len(filterDoc) > 0 {
			pipeline = append(pipeline, bson.D{{Key: "$match", Value: filterDoc}})
		}
	}

	// Step 6: apply user-supplied sort (or default).
	sortFields := query.DefaultSort()
	if q != nil && len(q.Sort) > 0 {
		sortFields = q.Sort
	}
	sortDoc := TranslateSort(sortFields)
	if len(sortDoc) > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$sort", Value: sortDoc}})
	}

	// Step 7: $facet — count + paginated slice.
	limit := 50
	offset := 0
	if q != nil {
		if q.Limit > 0 {
			limit = q.Limit
		}
		if q.Offset > 0 {
			offset = q.Offset
		}
	}

	facetItems := mongo.Pipeline{
		{{Key: "$skip", Value: offset}},
		{{Key: "$limit", Value: limit}},
	}
	facetCount := mongo.Pipeline{
		{{Key: "$count", Value: "total"}},
	}

	pipeline = append(pipeline, bson.D{{Key: "$facet", Value: bson.D{
		{Key: "items", Value: facetItems},
		{Key: "count", Value: facetCount},
	}}})

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, coreerrors.Internal("failed to execute list query", err)
	}
	defer cursor.Close(ctx)

	// Decode the single $facet result document.
	type countDoc struct {
		Total int64 `bson:"total"`
	}
	type facetResult struct {
		Items []model.Document `bson:"items"`
		Count []countDoc       `bson:"count"`
	}

	var results []facetResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, coreerrors.Internal("failed to decode list results", err)
	}

	if len(results) == 0 {
		return &model.ListResult{Items: []*model.Document{}, Total: 0, HasMore: false}, nil
	}

	facet := results[0]

	var total int64
	if len(facet.Count) > 0 {
		total = facet.Count[0].Total
	}

	items := make([]*model.Document, len(facet.Items))
	for i := range facet.Items {
		d := facet.Items[i]
		items[i] = &d
	}

	hasMore := int64(offset+len(items)) < total

	return &model.ListResult{
		Items:   items,
		Total:   total,
		HasMore: hasMore,
	}, nil
}

// Update applies a partial patch to a document, appending a new version.
func (r *MongoRepository) Update(ctx context.Context, schemaName, entityID string, data map[string]any) (*model.Document, error) {
	current, err := r.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return nil, err
	}

	merged := jsonutil.DeepMerge(current.Data, data)
	now := time.Now().UTC()

	newDoc := &model.Document{
		ID:            uuid.New().String(),
		EntityID:      current.EntityID,
		SchemaName:    schemaName,
		RecordVersion: current.RecordVersion + 1,
		SchemaVersion: current.SchemaVersion,
		Data:          merged,
		CreatedAt:     current.CreatedAt,
		CreatedBy:     current.CreatedBy,
		UpdatedAt:     now,
		ETag:          generateETag(merged),
	}

	coll := r.conn.Collection(collectionName(schemaName))
	if _, err := coll.InsertOne(ctx, newDoc); err != nil {
		return nil, coreerrors.Internal("failed to insert updated document version", err)
	}

	return newDoc, nil
}

// Delete soft-deletes a document by appending a tombstone version.
func (r *MongoRepository) Delete(ctx context.Context, schemaName, entityID string) error {
	current, err := r.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	tombstone := &model.Document{
		ID:            uuid.New().String(),
		EntityID:      current.EntityID,
		SchemaName:    schemaName,
		RecordVersion: current.RecordVersion + 1,
		SchemaVersion: current.SchemaVersion,
		Data:          current.Data,
		CreatedAt:     current.CreatedAt,
		CreatedBy:     current.CreatedBy,
		UpdatedAt:     current.UpdatedAt,
		DeletedAt:     &now,
		ETag:          current.ETag,
	}

	coll := r.conn.Collection(collectionName(schemaName))
	if _, err := coll.InsertOne(ctx, tombstone); err != nil {
		return coreerrors.Internal("failed to insert tombstone", err)
	}

	return nil
}

// BulkCreate persists multiple documents in order, returning results in
// input order. On any per-item error the operation stops and returns the error.
func (r *MongoRepository) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	results := make([]*model.Document, 0, len(items))
	for _, data := range items {
		doc, err := r.Create(ctx, schemaName, data)
		if err != nil {
			return results, fmt.Errorf("mongo: bulk create: %w", err)
		}
		results = append(results, doc)
	}
	return results, nil
}

// BulkUpdate applies per-item patches to existing documents.
// Items whose entity IDs cannot be found are skipped.
func (r *MongoRepository) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	results := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := r.Update(ctx, schemaName, item.EntityID, item.Data)
		if err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return results, fmt.Errorf("mongo: bulk update: %w", err)
		}
		results = append(results, doc)
	}
	return results, nil
}

// BulkDelete soft-deletes the documents identified by ids.
// IDs that do not exist are silently skipped.
func (r *MongoRepository) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	for _, id := range ids {
		if err := r.Delete(ctx, schemaName, id); err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("mongo: bulk delete: %w", err)
		}
	}
	return nil
}

// EnsureIndexes creates the standard compound unique index on
// (entity_id, record_version) and a descending index on created_at.
// It is idempotent and safe to call on every startup.
func (r *MongoRepository) EnsureIndexes(ctx context.Context, schemaName string) error {
	coll := r.conn.Collection(collectionName(schemaName))

	models := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "entity_id", Value: 1},
				{Key: "record_version", Value: -1},
			},
			Options: mongooptions.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "created_at", Value: -1},
			},
		},
	}

	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		return coreerrors.Internal("failed to ensure indexes", err)
	}

	return nil
}
