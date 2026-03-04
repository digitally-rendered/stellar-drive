package sql

import (
	"strings"
	"testing"
)

// TestTableName verifies that schema names are pluralised and lower-cased.
func TestTableName(t *testing.T) {
	tests := []struct {
		schemaName string
		want       string
	}{
		{"user", "users"},
		{"product", "products"},
		{"category", "categories"},
		{"document", "documents"},
		{"church", "churches"},
	}
	for _, tt := range tests {
		t.Run(tt.schemaName, func(t *testing.T) {
			got := tableName(tt.schemaName)
			if got != tt.want {
				t.Errorf("tableName(%q) = %q, want %q", tt.schemaName, got, tt.want)
			}
		})
	}
}

// TestLatestVersionSubquery verifies structural properties of the generated
// subquery without executing it against a real database.
func TestLatestVersionSubquery(t *testing.T) {
	tests := []struct {
		schemaName   string
		dialect      Dialect
		wantContains []string
	}{
		{
			schemaName: "user",
			dialect:    PostgresDialect{},
			wantContains: []string{
				`"users"`,
				"MAX(record_version)",
				"GROUP BY entity_id",
				"entity_id",
				"max_version",
			},
		},
		{
			schemaName: "product",
			dialect:    SQLiteDialect{},
			wantContains: []string{
				`"products"`,
				"MAX(record_version)",
				"GROUP BY entity_id",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.schemaName, func(t *testing.T) {
			got := latestVersionSubquery(tt.schemaName, tt.dialect)
			for _, substr := range tt.wantContains {
				if !strings.Contains(got, substr) {
					t.Errorf("subquery missing %q\nfull subquery:\n%s", substr, got)
				}
			}
		})
	}
}
