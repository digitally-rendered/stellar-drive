package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_DefaultMongo(t *testing.T) {
	c := NewConfig()

	got := c.Get("any_schema")

	assert.Equal(t, Mongo, got, "unmapped schema should fall back to Mongo default")
}

func TestConfig_WithDefaultSQL(t *testing.T) {
	c := NewConfig(WithDefault(SQL))

	got := c.Get("any_schema")

	assert.Equal(t, SQL, got, "unmapped schema should return the configured default")
}

func TestConfig_ExplicitMapping(t *testing.T) {
	tests := []struct {
		name       string
		schemaName string
		backend    Backend
	}{
		{
			name:       "explicit mongo",
			schemaName: "orders",
			backend:    Mongo,
		},
		{
			name:       "explicit sql",
			schemaName: "users",
			backend:    SQL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfig()
			require.NoError(t, c.Set(tc.schemaName, tc.backend))

			got := c.Get(tc.schemaName)

			assert.Equal(t, tc.backend, got)
		})
	}
}

func TestConfig_ExplicitMappingOverridesDefault(t *testing.T) {
	// Default is SQL, but "pets" is explicitly wired to Mongo.
	c := NewConfig(WithDefault(SQL))
	require.NoError(t, c.Set("pets", Mongo))

	assert.Equal(t, Mongo, c.Get("pets"), "explicit mapping should override default")
	assert.Equal(t, SQL, c.Get("orders"), "unmapped schema should still use default")
}

func TestConfig_SetInvalidBackend(t *testing.T) {
	c := NewConfig()

	err := c.Set("pets", Backend("redis"))

	assert.Error(t, err, "unknown backend should return an error")
	assert.Contains(t, err.Error(), "redis")
}

func TestConfig_SQLSchemas(t *testing.T) {
	c := NewConfig()
	require.NoError(t, c.Set("users", SQL))
	require.NoError(t, c.Set("products", SQL))
	require.NoError(t, c.Set("pets", Mongo))

	got := c.SQLSchemas()

	assert.Equal(t, []string{"products", "users"}, got,
		"SQLSchemas should return only SQL-mapped schemas in lexicographic order")
}

func TestConfig_MongoSchemas(t *testing.T) {
	c := NewConfig()
	require.NoError(t, c.Set("pets", Mongo))
	require.NoError(t, c.Set("orders", Mongo))
	require.NoError(t, c.Set("users", SQL))

	got := c.MongoSchemas()

	assert.Equal(t, []string{"orders", "pets"}, got,
		"MongoSchemas should return only Mongo-mapped schemas in lexicographic order")
}

func TestConfig_EmptyMappings(t *testing.T) {
	c := NewConfig()

	assert.Empty(t, c.SQLSchemas(), "no explicit mappings means SQLSchemas is empty")
	assert.Empty(t, c.MongoSchemas(), "no explicit mappings means MongoSchemas is empty")
}

func TestConfig_SetValidBackends(t *testing.T) {
	tests := []struct {
		name    string
		backend Backend
	}{
		{"mongo", Mongo},
		{"sql", SQL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfig()
			err := c.Set("some_schema", tc.backend)
			assert.NoError(t, err, "known backend %q should not error", tc.backend)
		})
	}
}
