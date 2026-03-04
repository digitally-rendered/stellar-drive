package mongo

import (
	"context"
	"fmt"
	"time"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

const schemasCollection = "_schemas"

// MongoSchemaStore implements schema.Store backed by the "_schemas" collection.
// Schemas are stored as SchemaEnvelope documents. Multiple versions of the same
// schema name may coexist; the highest version is treated as current.
type MongoSchemaStore struct {
	conn *Connection
}

// NewSchemaStore returns a MongoSchemaStore that uses the given Connection.
func NewSchemaStore(conn *Connection) *MongoSchemaStore {
	return &MongoSchemaStore{conn: conn}
}

func (s *MongoSchemaStore) coll() *mongo.Collection {
	return s.conn.Collection(schemasCollection)
}

// Save persists or updates a schema envelope. When a document with the same
// name and version already exists it is replaced; otherwise a new document is
// inserted.
func (s *MongoSchemaStore) Save(ctx context.Context, envelope *schema.SchemaEnvelope) error {
	if envelope == nil {
		return coreerrors.BadRequest("schema envelope must not be nil")
	}

	now := time.Now().UTC()
	envelope.UpdatedAt = now
	if envelope.CreatedAt.IsZero() {
		envelope.CreatedAt = now
	}
	envelope.Active = true

	filter := bson.D{
		{Key: "name", Value: envelope.Name},
		{Key: "version", Value: envelope.Version},
	}

	upsert := true
	opts := mongooptions.Replace().SetUpsert(upsert)

	if _, err := s.coll().ReplaceOne(ctx, filter, envelope, opts); err != nil {
		return coreerrors.Internal("failed to save schema", err)
	}

	return nil
}

// Get retrieves a schema envelope by name. When version is empty the latest
// active version is returned (sorted by version descending). When version is
// specified the exact version is required.
func (s *MongoSchemaStore) Get(ctx context.Context, name, version string) (*schema.SchemaEnvelope, error) {
	filter := bson.D{
		{Key: "name", Value: name},
		{Key: "active", Value: true},
	}
	if version != "" {
		filter = append(filter, bson.E{Key: "version", Value: version})
	}

	opts := mongooptions.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})

	var env schema.SchemaEnvelope
	err := s.coll().FindOne(ctx, filter, opts).Decode(&env)
	if err == mongo.ErrNoDocuments {
		if version != "" {
			return nil, coreerrors.SchemaNotFound(fmt.Sprintf("%s@%s", name, version))
		}
		return nil, coreerrors.SchemaNotFound(name)
	}
	if err != nil {
		return nil, coreerrors.Internal("failed to get schema", err)
	}

	return &env, nil
}

// List returns the latest active version of every registered schema name.
func (s *MongoSchemaStore) List(ctx context.Context) ([]*schema.SchemaEnvelope, error) {
	// Aggregate: filter active → sort by version desc → group by name → first doc per name.
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "active", Value: true}}}},
		{{Key: "$sort", Value: bson.D{{Key: "version", Value: -1}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$name"},
			{Key: "doc", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		{{Key: "$replaceRoot", Value: bson.D{{Key: "newRoot", Value: "$doc"}}}},
		{{Key: "$sort", Value: bson.D{{Key: "name", Value: 1}}}},
	}

	cursor, err := s.coll().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, coreerrors.Internal("failed to list schemas", err)
	}
	defer cursor.Close(ctx)

	var envelopes []schema.SchemaEnvelope
	if err := cursor.All(ctx, &envelopes); err != nil {
		return nil, coreerrors.Internal("failed to decode schema list", err)
	}

	result := make([]*schema.SchemaEnvelope, len(envelopes))
	for i := range envelopes {
		e := envelopes[i]
		result[i] = &e
	}

	return result, nil
}

// ListVersions returns all versions of the named schema ordered by version
// descending (newest first).
func (s *MongoSchemaStore) ListVersions(ctx context.Context, name string) ([]*schema.SchemaEnvelope, error) {
	filter := bson.D{{Key: "name", Value: name}}
	opts := mongooptions.Find().SetSort(bson.D{{Key: "version", Value: -1}})

	cursor, err := s.coll().Find(ctx, filter, opts)
	if err != nil {
		return nil, coreerrors.Internal("failed to list schema versions", err)
	}
	defer cursor.Close(ctx)

	var envelopes []schema.SchemaEnvelope
	if err := cursor.All(ctx, &envelopes); err != nil {
		return nil, coreerrors.Internal("failed to decode schema versions", err)
	}

	result := make([]*schema.SchemaEnvelope, len(envelopes))
	for i := range envelopes {
		e := envelopes[i]
		result[i] = &e
	}

	return result, nil
}

// Delete soft-deletes all versions of the named schema by setting active=false.
func (s *MongoSchemaStore) Delete(ctx context.Context, name string) error {
	filter := bson.D{{Key: "name", Value: name}}
	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "active", Value: false},
		{Key: "updated_at", Value: time.Now().UTC()},
	}}}

	if _, err := s.coll().UpdateMany(ctx, filter, update); err != nil {
		return coreerrors.Internal("failed to delete schema", err)
	}

	return nil
}

// Watch opens a MongoDB change stream on the _schemas collection and emits
// SchemaEvents on the returned channel. The channel is closed when ctx is
// cancelled or the change stream is exhausted.
//
// The caller must drain the channel to avoid blocking the goroutine.
func (s *MongoSchemaStore) Watch(ctx context.Context) (<-chan schema.SchemaEvent, error) {
	// Watch only insert, replace, and update operations on the schemas collection.
	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "operationType", Value: bson.D{{Key: "$in", Value: bson.A{"insert", "replace", "update"}}}},
	}}}
	pipeline := mongo.Pipeline{matchStage}

	opts := mongooptions.ChangeStream().SetFullDocument(mongooptions.UpdateLookup)
	stream, err := s.coll().Watch(ctx, pipeline, opts)
	if err != nil {
		return nil, coreerrors.Internal("failed to open schema change stream", err)
	}

	ch := make(chan schema.SchemaEvent, 32)

	go func() {
		defer close(ch)
		defer stream.Close(ctx)

		for stream.Next(ctx) {
			var event changeStreamEvent
			if err := stream.Decode(&event); err != nil {
				continue
			}

			schemaEvent := mapChangeToSchemaEvent(event)
			if schemaEvent == nil {
				continue
			}

			select {
			case ch <- *schemaEvent:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// changeStreamEvent holds the fields we need from a MongoDB change event.
type changeStreamEvent struct {
	OperationType string                 `bson:"operationType"`
	FullDocument  *schema.SchemaEnvelope `bson:"fullDocument"`
}

// mapChangeToSchemaEvent converts a raw change stream event into a SchemaEvent.
// Returns nil when the event type is not relevant or the full document is absent.
func mapChangeToSchemaEvent(event changeStreamEvent) *schema.SchemaEvent {
	if event.FullDocument == nil {
		return nil
	}

	var eventType schema.SchemaEventType
	switch event.OperationType {
	case "insert":
		eventType = schema.SchemaRegistered
	case "replace", "update":
		if !event.FullDocument.Active {
			eventType = schema.SchemaDeactivated
		} else {
			eventType = schema.SchemaUpdated
		}
	default:
		return nil
	}

	return &schema.SchemaEvent{
		Type:       eventType,
		Envelope:   event.FullDocument,
		OccurredAt: time.Now().UTC(),
	}
}
