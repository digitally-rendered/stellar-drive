package mongo

import (
	"context"
	"fmt"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoAuditStore implements port.AuditStore using a dedicated MongoDB collection.
type MongoAuditStore struct {
	conn           *Connection
	collectionName string
}

// AuditStoreOption configures a MongoAuditStore.
type AuditStoreOption func(*MongoAuditStore)

// WithAuditCollection sets the collection name for audit entries.
func WithAuditCollection(name string) AuditStoreOption {
	return func(s *MongoAuditStore) {
		s.collectionName = name
	}
}

// NewAuditStore returns a MongoAuditStore that uses the given Connection.
func NewAuditStore(conn *Connection, opts ...AuditStoreOption) *MongoAuditStore {
	s := &MongoAuditStore{
		conn:           conn,
		collectionName: "audit_log",
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Record persists a single audit entry.
func (s *MongoAuditStore) Record(ctx context.Context, entry *model.AuditEntry) error {
	coll := s.conn.Collection(s.collectionName)
	if _, err := coll.InsertOne(ctx, entry); err != nil {
		return fmt.Errorf("audit store: insert: %w", err)
	}
	return nil
}

// GetHistory returns audit entries for the given entity ID, ordered by
// timestamp descending.
func (s *MongoAuditStore) GetHistory(ctx context.Context, entityID string, opts ...port.AuditQueryOption) ([]*model.AuditEntry, error) {
	q := port.BuildAuditQuery(opts...)
	filter := bson.D{{Key: "entity_id", Value: entityID}}
	if q.SchemaName != "" {
		filter = append(filter, bson.E{Key: "schema_name", Value: q.SchemaName})
	}
	return s.find(ctx, filter, q.Limit)
}

// GetUserActivity returns audit entries for the given user ID, ordered by
// timestamp descending.
func (s *MongoAuditStore) GetUserActivity(ctx context.Context, userID string, opts ...port.AuditQueryOption) ([]*model.AuditEntry, error) {
	q := port.BuildAuditQuery(opts...)
	filter := bson.D{{Key: "user_id", Value: userID}}
	if q.SchemaName != "" {
		filter = append(filter, bson.E{Key: "schema_name", Value: q.SchemaName})
	}
	return s.find(ctx, filter, q.Limit)
}

func (s *MongoAuditStore) find(ctx context.Context, filter bson.D, limit int) ([]*model.AuditEntry, error) {
	coll := s.conn.Collection(s.collectionName)

	findOpts := mongooptions.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	if limit > 0 {
		findOpts.SetLimit(int64(limit))
	}

	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("audit store: find: %w", err)
	}
	defer cursor.Close(ctx)

	var entries []*model.AuditEntry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, fmt.Errorf("audit store: decode: %w", err)
	}

	if entries == nil {
		entries = []*model.AuditEntry{}
	}
	return entries, nil
}
