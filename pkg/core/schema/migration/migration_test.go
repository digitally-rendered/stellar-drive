package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
)

func TestRegistry_RegisterValidation(t *testing.T) {
	reg := NewRegistry()
	noop := func(_ context.Context, d map[string]any) (map[string]any, error) { return d, nil }

	cases := []struct {
		name     string
		schema   string
		from, to string
		fn       Func
		wantErr  bool
	}{
		{"empty schema", "", "1.0.0", "1.1.0", noop, true},
		{"empty from", "pet", "", "1.1.0", noop, true},
		{"empty to", "pet", "1.0.0", "", noop, true},
		{"same from and to", "pet", "1.0.0", "1.0.0", noop, true},
		{"nil fn", "pet", "1.0.0", "1.1.0", nil, true},
		{"valid", "pet", "1.0.0", "1.1.0", noop, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.Register(tc.schema, tc.from, tc.to, tc.fn)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegistry_ChainSingleStep(t *testing.T) {
	reg := NewRegistry()
	called := false
	require.NoError(t, reg.Register("pet", "1.0.0", "1.1.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			called = true
			d["migrated"] = true
			return d, nil
		}))

	chain, err := reg.Chain("pet", "1.0.0", "1.1.0")
	require.NoError(t, err)
	require.Len(t, chain, 1)

	data, err := chain[0](context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, true, data["migrated"])
}

func TestRegistry_ChainNoMigrationNeeded(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "1.1.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) { return d, nil }))

	chain, err := reg.Chain("pet", "1.0.0", "1.0.0")
	require.NoError(t, err)
	assert.Len(t, chain, 0)
}

func TestRegistry_ChainMultiStep(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "1.1.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["step_1_1"] = true
			return d, nil
		}))
	require.NoError(t, reg.Register("pet", "1.1.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["step_2_0"] = true
			return d, nil
		}))

	chain, err := reg.Chain("pet", "1.0.0", "2.0.0")
	require.NoError(t, err)
	require.Len(t, chain, 2)

	data := map[string]any{}
	for _, fn := range chain {
		data, err = fn(context.Background(), data)
		require.NoError(t, err)
	}
	assert.Equal(t, true, data["step_1_1"])
	assert.Equal(t, true, data["step_2_0"])
}

func TestRegistry_ChainPrefersLongerEdges(t *testing.T) {
	reg := NewRegistry()
	// Both a small step and a direct jump exist. The direct jump should be
	// preferred because it does not overshoot the target.
	require.NoError(t, reg.Register("pet", "1.0.0", "1.1.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["step_1_1"] = true
			return d, nil
		}))
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["step_2_0"] = true
			return d, nil
		}))

	chain, err := reg.Chain("pet", "1.0.0", "2.0.0")
	require.NoError(t, err)
	require.Len(t, chain, 1, "should pick the direct 1.0.0 -> 2.0.0 jump")

	data, err := chain[0](context.Background(), map[string]any{})
	require.NoError(t, err)
	_, visited11 := data["step_1_1"]
	assert.False(t, visited11)
	assert.Equal(t, true, data["step_2_0"])
}

func TestRegistry_ChainFailsWhenNoEdge(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "1.1.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) { return d, nil }))

	_, err := reg.Chain("pet", "1.0.0", "3.0.0")
	assert.Error(t, err)

	_, err = reg.Chain("order", "1.0.0", "2.0.0")
	assert.Error(t, err)
}

func TestRegistry_ApplyMutatesDocument(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			name, _ := d["name"].(string)
			d["first_name"] = name
			delete(d, "name")
			return d, nil
		}))

	doc := &model.Document{
		SchemaName:    "pet",
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "rex"},
	}

	require.NoError(t, reg.Apply(context.Background(), doc, "2.0.0"))

	assert.Equal(t, "2.0.0", doc.SchemaVersion)
	assert.Equal(t, "rex", doc.Data["first_name"])
	_, hasOld := doc.Data["name"]
	assert.False(t, hasOld)
}

func TestRegistry_ApplyNoopWhenAlreadyCurrent(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, d map[string]any) (map[string]any, error) {
			d["migrated"] = true
			return d, nil
		}))

	doc := &model.Document{
		SchemaName:    "pet",
		SchemaVersion: "2.0.0",
		Data:          map[string]any{"name": "rex"},
	}
	require.NoError(t, reg.Apply(context.Background(), doc, "2.0.0"))
	assert.Equal(t, "2.0.0", doc.SchemaVersion)
	_, migrated := doc.Data["migrated"]
	assert.False(t, migrated, "already-current docs should not be touched")
}

func TestRegistry_ApplyNoopWhenNoMigrationsRegistered(t *testing.T) {
	reg := NewRegistry()
	doc := &model.Document{
		SchemaName:    "pet",
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "rex"},
	}
	require.NoError(t, reg.Apply(context.Background(), doc, "2.0.0"))
	assert.Equal(t, "1.0.0", doc.SchemaVersion, "schema version must not be bumped without a migration")
}

func TestRegistry_ApplyPropagatesStepError(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register("pet", "1.0.0", "2.0.0",
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, assert.AnError
		}))

	doc := &model.Document{
		SchemaName:    "pet",
		SchemaVersion: "1.0.0",
		Data:          map[string]any{},
	}
	err := reg.Apply(context.Background(), doc, "2.0.0")
	assert.Error(t, err)
	assert.Equal(t, "1.0.0", doc.SchemaVersion, "failed migration must not update the version")
}
