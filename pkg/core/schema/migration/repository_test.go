package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// fakeRepo is a minimal port.Repository stub used to verify the migrating
// decorator calls through and applies migrations to returned documents.
type fakeRepo struct {
	findByID func(ctx context.Context, schemaName, id string) (*model.Document, error)
	list     func(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)
}

var _ port.Repository = (*fakeRepo)(nil)

func (f *fakeRepo) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	return &model.Document{SchemaName: schemaName, SchemaVersion: "2.0.0", Data: data}, nil
}
func (f *fakeRepo) FindByID(ctx context.Context, schemaName, id string) (*model.Document, error) {
	return f.findByID(ctx, schemaName, id)
}
func (f *fakeRepo) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	return f.list(ctx, schemaName, q)
}
func (f *fakeRepo) Update(ctx context.Context, schemaName, id string, data map[string]any) (*model.Document, error) {
	return nil, nil
}
func (f *fakeRepo) Delete(ctx context.Context, schemaName, id string) error { return nil }
func (f *fakeRepo) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	return nil, nil
}
func (f *fakeRepo) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	return nil, nil
}
func (f *fakeRepo) BulkDelete(ctx context.Context, schemaName string, ids []string) error { return nil }
func (f *fakeRepo) EnsureIndexes(ctx context.Context, schemaName string) error            { return nil }

func TestRepository_FindByIDRunsMigration(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			if name, ok := d["name"].(string); ok {
				d["first_name"] = name
				delete(d, "name")
			}
			return d, nil
		}))

	inner := &fakeRepo{
		findByID: func(_ context.Context, _, id string) (*model.Document, error) {
			return &model.Document{
				SchemaName:    "pet",
				SchemaVersion: "1.0.0",
				Data:          map[string]any{"name": "rex"},
			}, nil
		},
	}
	repo := NewRepository(inner, reg, func(_ string) string { return "2.0.0" })

	doc, err := repo.FindByID(context.Background(), "pet", "some-id")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", doc.SchemaVersion)
	assert.Equal(t, "rex", doc.Data["first_name"])
}

func TestRepository_ListMigratesAllItems(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["migrated"] = true
			return d, nil
		}))

	inner := &fakeRepo{
		list: func(_ context.Context, _ string, _ *query.Query) (*model.ListResult, error) {
			return &model.ListResult{
				Items: []*model.Document{
					{SchemaName: "pet", SchemaVersion: "1.0.0", Data: map[string]any{}},
					{SchemaName: "pet", SchemaVersion: "2.0.0", Data: map[string]any{}},
				},
			}, nil
		},
	}
	repo := NewRepository(inner, reg, func(_ string) string { return "2.0.0" })

	res, err := repo.List(context.Background(), "pet", nil)
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	assert.Equal(t, true, res.Items[0].Data["migrated"])
	_, alreadyCurrentTouched := res.Items[1].Data["migrated"]
	assert.False(t, alreadyCurrentTouched, "current-version docs should be untouched")
}

func TestRepository_PassesThroughWhenCurrentVersionEmpty(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["should_not_run"] = true
			return d, nil
		}))

	inner := &fakeRepo{
		findByID: func(_ context.Context, _, _ string) (*model.Document, error) {
			return &model.Document{
				SchemaName:    "pet",
				SchemaVersion: "1.0.0",
				Data:          map[string]any{},
			}, nil
		},
	}
	// currentVer returns empty → no migration attempted.
	repo := NewRepository(inner, reg, func(_ string) string { return "" })

	doc, err := repo.FindByID(context.Background(), "pet", "id")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", doc.SchemaVersion)
	_, migrated := doc.Data["should_not_run"]
	assert.False(t, migrated)
}
