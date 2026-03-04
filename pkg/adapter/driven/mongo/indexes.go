package mongo

import (
	"context"
	"fmt"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CreateSchemaIndexes creates user-defined indexes declared in the schema's
// IndexDef slice in addition to the standard versioning indexes. It is
// idempotent: MongoDB silently skips index creation when an identical index
// already exists.
func (r *MongoRepository) CreateSchemaIndexes(ctx context.Context, schemaName string, indexes []schema.IndexDef) error {
	// Always ensure the standard versioning indexes first.
	if err := r.EnsureIndexes(ctx, schemaName); err != nil {
		return err
	}

	if len(indexes) == 0 {
		return nil
	}

	coll := r.conn.Collection(collectionName(schemaName))
	models := make([]mongo.IndexModel, 0, len(indexes))

	for i, def := range indexes {
		if len(def.Fields) == 0 {
			return coreerrors.BadRequest(fmt.Sprintf("index %d has no fields", i))
		}

		keys := make(bson.D, 0, len(def.Fields))
		for _, field := range def.Fields {
			keys = append(keys, bson.E{Key: fieldKey(field), Value: 1})
		}

		idxOpts := mongooptions.Index()
		if def.Unique {
			idxOpts = idxOpts.SetUnique(true)
		}
		if def.Sparse {
			idxOpts = idxOpts.SetSparse(true)
		}

		models = append(models, mongo.IndexModel{
			Keys:    keys,
			Options: idxOpts,
		})
	}

	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		return coreerrors.Internal("failed to create schema indexes", err)
	}

	return nil
}
