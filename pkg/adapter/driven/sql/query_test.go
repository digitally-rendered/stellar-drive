package sql

import (
	"strings"
	"testing"

	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// TestTranslateFilter verifies that FilterNode trees are correctly translated
// into SQL WHERE clause fragments for both dialects.
func TestTranslateFilter(t *testing.T) {
	pg := PostgresDialect{}
	sl := SQLiteDialect{}

	tests := []struct {
		name       string
		node       *query.FilterNode
		dialect    Dialect
		wantClause string
		wantArgs   []any
	}{
		{
			name:       "nil node",
			node:       nil,
			dialect:    pg,
			wantClause: "",
			wantArgs:   nil,
		},
		// --- Equality ---
		{
			name: "eq postgres",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpEq, Value: "alice",
			},
			dialect:    pg,
			wantClause: "data->>'name' = $1",
			wantArgs:   []any{"alice"},
		},
		{
			name: "eq sqlite",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpEq, Value: "alice",
			},
			dialect:    sl,
			wantClause: "json_extract(data, '$.name') = ?",
			wantArgs:   []any{"alice"},
		},
		// --- Audit field (no JSON extraction) ---
		{
			name: "eq on audit field",
			node: &query.FilterNode{
				Field: "entity_id", Operator: query.OpEq, Value: "abc-123",
			},
			dialect:    pg,
			wantClause: "entity_id = $1",
			wantArgs:   []any{"abc-123"},
		},
		// --- Inequality ---
		{
			name: "neq",
			node: &query.FilterNode{
				Field: "status", Operator: query.OpNeq, Value: "deleted",
			},
			dialect:    pg,
			wantClause: "data->>'status' <> $1",
			wantArgs:   []any{"deleted"},
		},
		// --- Comparisons ---
		{
			name: "gt",
			node: &query.FilterNode{Field: "age", Operator: query.OpGt, Value: 18},
			dialect:    pg,
			wantClause: "data->>'age' > $1",
			wantArgs:   []any{18},
		},
		{
			name: "gte",
			node: &query.FilterNode{Field: "age", Operator: query.OpGte, Value: 21},
			dialect:    pg,
			wantClause: "data->>'age' >= $1",
			wantArgs:   []any{21},
		},
		{
			name: "lt",
			node: &query.FilterNode{Field: "score", Operator: query.OpLt, Value: 100},
			dialect:    pg,
			wantClause: "data->>'score' < $1",
			wantArgs:   []any{100},
		},
		{
			name: "lte",
			node: &query.FilterNode{Field: "score", Operator: query.OpLte, Value: 50},
			dialect:    pg,
			wantClause: "data->>'score' <= $1",
			wantArgs:   []any{50},
		},
		// --- IN / NOT IN ---
		{
			name: "in postgres",
			node: &query.FilterNode{
				Field:    "status",
				Operator: query.OpIn,
				Value:    []any{"active", "pending"},
			},
			dialect:    pg,
			wantClause: "data->>'status' IN ($1, $2)",
			wantArgs:   []any{"active", "pending"},
		},
		{
			name: "in sqlite",
			node: &query.FilterNode{
				Field:    "status",
				Operator: query.OpIn,
				Value:    []any{"active", "pending"},
			},
			dialect:    sl,
			wantClause: "json_extract(data, '$.status') IN (?, ?)",
			wantArgs:   []any{"active", "pending"},
		},
		{
			name: "in empty list",
			node: &query.FilterNode{
				Field:    "status",
				Operator: query.OpIn,
				Value:    []any{},
			},
			dialect:    pg,
			wantClause: "1 = 0",
			wantArgs:   nil,
		},
		{
			name: "nin",
			node: &query.FilterNode{
				Field:    "status",
				Operator: query.OpNin,
				Value:    []any{"deleted"},
			},
			dialect:    pg,
			wantClause: "data->>'status' NOT IN ($1)",
			wantArgs:   []any{"deleted"},
		},
		{
			name: "nin empty list",
			node: &query.FilterNode{
				Field:    "status",
				Operator: query.OpNin,
				Value:    []any{},
			},
			dialect:    pg,
			wantClause: "1 = 1",
			wantArgs:   nil,
		},
		// --- LIKE / ILIKE ---
		{
			name: "like",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpLike, Value: "ali%",
			},
			dialect:    pg,
			wantClause: "data->>'name' LIKE $1",
			wantArgs:   []any{"ali%"},
		},
		{
			name: "ilike postgres",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpILike, Value: "ali%",
			},
			dialect:    pg,
			wantClause: "data->>'name' ILIKE $1",
			wantArgs:   []any{"ali%"},
		},
		{
			name: "ilike sqlite fallback",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpILike, Value: "ali%",
			},
			dialect:    sl,
			wantClause: "json_extract(data, '$.name') LIKE ?",
			wantArgs:   []any{"ali%"},
		},
		// --- Contains / StartsWith / EndsWith ---
		{
			name: "contains",
			node: &query.FilterNode{
				Field: "bio", Operator: query.OpContains, Value: "engineer",
			},
			dialect:    pg,
			wantClause: "data->>'bio' LIKE $1",
			wantArgs:   []any{"%engineer%"},
		},
		{
			name: "startswith",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpStartsWith, Value: "Al",
			},
			dialect:    pg,
			wantClause: "data->>'name' LIKE $1",
			wantArgs:   []any{"Al%"},
		},
		{
			name: "endswith",
			node: &query.FilterNode{
				Field: "name", Operator: query.OpEndsWith, Value: "son",
			},
			dialect:    pg,
			wantClause: "data->>'name' LIKE $1",
			wantArgs:   []any{"%son"},
		},
		// --- Exists ---
		{
			name: "exists true",
			node: &query.FilterNode{
				Field: "phone", Operator: query.OpExists, Value: true,
			},
			dialect:    pg,
			wantClause: "data->>'phone' IS NOT NULL",
			wantArgs:   nil,
		},
		{
			name: "exists false",
			node: &query.FilterNode{
				Field: "phone", Operator: query.OpExists, Value: false,
			},
			dialect:    pg,
			wantClause: "data->>'phone' IS NULL",
			wantArgs:   nil,
		},
		{
			name: "exists on audit col always true",
			node: &query.FilterNode{
				Field: "entity_id", Operator: query.OpExists, Value: true,
			},
			dialect:    pg,
			wantClause: "1 = 1",
			wantArgs:   nil,
		},
		{
			name: "exists on audit col always false",
			node: &query.FilterNode{
				Field: "entity_id", Operator: query.OpExists, Value: false,
			},
			dialect:    pg,
			wantClause: "1 = 0",
			wantArgs:   nil,
		},
		// --- IsNull ---
		{
			name: "is_null true",
			node: &query.FilterNode{
				Field: "deleted_at", Operator: query.OpIsNull, Value: true,
			},
			dialect:    pg,
			wantClause: "deleted_at IS NULL",
			wantArgs:   nil,
		},
		{
			name: "is_null false",
			node: &query.FilterNode{
				Field: "deleted_at", Operator: query.OpIsNull, Value: false,
			},
			dialect:    pg,
			wantClause: "deleted_at IS NOT NULL",
			wantArgs:   nil,
		},
		// --- Logical: AND ---
		{
			name: "and",
			node: &query.FilterNode{
				Operator: query.OpAnd,
				Children: []*query.FilterNode{
					{Field: "name", Operator: query.OpEq, Value: "alice"},
					{Field: "age", Operator: query.OpGt, Value: 18},
				},
			},
			dialect:    pg,
			wantClause: "(data->>'name' = $1) AND (data->>'age' > $2)",
			wantArgs:   []any{"alice", 18},
		},
		// --- Logical: OR ---
		{
			name: "or",
			node: &query.FilterNode{
				Operator: query.OpOr,
				Children: []*query.FilterNode{
					{Field: "status", Operator: query.OpEq, Value: "active"},
					{Field: "status", Operator: query.OpEq, Value: "pending"},
				},
			},
			dialect:    pg,
			wantClause: "(data->>'status' = $1) OR (data->>'status' = $2)",
			wantArgs:   []any{"active", "pending"},
		},
		// --- Logical: NOT ---
		{
			name: "not",
			node: &query.FilterNode{
				Operator: query.OpNot,
				Children: []*query.FilterNode{
					{Field: "status", Operator: query.OpEq, Value: "deleted"},
				},
			},
			dialect:    pg,
			wantClause: "NOT ((data->>'status' = $1))",
			wantArgs:   []any{"deleted"},
		},
		// --- Logical with empty children ---
		{
			name: "and empty children",
			node: &query.FilterNode{
				Operator: query.OpAnd,
				Children: []*query.FilterNode{},
			},
			dialect:    pg,
			wantClause: "",
			wantArgs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := TranslateFilter(tt.node, tt.dialect)

			if clause != tt.wantClause {
				t.Errorf("clause:\n  got  %q\n  want %q", clause, tt.wantClause)
			}

			if len(args) != len(tt.wantArgs) {
				t.Errorf("len(args) = %d, want %d; args = %v", len(args), len(tt.wantArgs), args)
				return
			}
			for i, a := range args {
				if a != tt.wantArgs[i] {
					t.Errorf("args[%d] = %v (%T), want %v (%T)", i, a, a, tt.wantArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

// TestTranslateSort verifies ORDER BY clause generation for both dialects.
func TestTranslateSort(t *testing.T) {
	pg := PostgresDialect{}
	sl := SQLiteDialect{}

	tests := []struct {
		name    string
		fields  []query.SortField
		dialect Dialect
		want    string
	}{
		{
			name:    "empty fields",
			fields:  nil,
			dialect: pg,
			want:    "",
		},
		{
			name: "single field asc postgres",
			fields: []query.SortField{
				{Field: "name", Direction: query.SortAsc},
			},
			dialect: pg,
			want:    "data->>'name' ASC",
		},
		{
			name: "single field desc sqlite",
			fields: []query.SortField{
				{Field: "name", Direction: query.SortDesc},
			},
			dialect: sl,
			want:    "json_extract(data, '$.name') DESC",
		},
		{
			name: "audit field sort",
			fields: []query.SortField{
				{Field: "created_at", Direction: query.SortDesc},
			},
			dialect: pg,
			want:    "created_at DESC",
		},
		{
			name: "multiple fields",
			fields: []query.SortField{
				{Field: "created_at", Direction: query.SortDesc},
				{Field: "name", Direction: query.SortAsc},
			},
			dialect: pg,
			want:    "created_at DESC, data->>'name' ASC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateSort(tt.fields, tt.dialect)
			if got != tt.want {
				t.Errorf("TranslateSort:\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

// TestToSlice exercises the toSlice helper for various value types.
func TestToSlice(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  []any
	}{
		{"nil", nil, nil},
		{"[]any", []any{1, "two"}, []any{1, "two"}},
		{"[]string", []string{"a", "b"}, []any{"a", "b"}},
		{"[]int", []int{1, 2, 3}, []any{1, 2, 3}},
		{"[]int64", []int64{10, 20}, []any{int64(10), int64(20)}},
		{"[]float64", []float64{1.1, 2.2}, []any{1.1, 2.2}},
		{"scalar", "hello", []any{"hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toSlice(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %v (%T), want %v (%T)", i, got[i], got[i], tt.want[i], tt.want[i])
				}
			}
		})
	}
}

// TestColumnExpr verifies that audit fields are kept as-is and data fields are
// wrapped in the appropriate JSON extraction expression.
func TestColumnExpr(t *testing.T) {
	pg := PostgresDialect{}
	sl := SQLiteDialect{}

	auditFieldNames := []string{
		"id", "entity_id", "schema_name", "record_version", "schema_version",
		"created_at", "updated_at", "deleted_at", "created_by", "updated_by",
		"deleted_by", "etag",
	}

	for _, f := range auditFieldNames {
		t.Run("audit/"+f, func(t *testing.T) {
			if got := columnExpr(f, pg); got != f {
				t.Errorf("columnExpr(%q, pg) = %q, want %q", f, got, f)
			}
			if got := columnExpr(f, sl); got != f {
				t.Errorf("columnExpr(%q, sl) = %q, want %q", f, got, f)
			}
		})
	}

	dataFields := []struct {
		field   string
		wantPG  string
		wantSQL string
	}{
		{"name", "data->>'name'", "json_extract(data, '$.name')"},
		{"user_age", "data->>'user_age'", "json_extract(data, '$.user_age')"},
	}

	for _, tt := range dataFields {
		t.Run("data/"+tt.field, func(t *testing.T) {
			if got := columnExpr(tt.field, pg); got != tt.wantPG {
				t.Errorf("columnExpr(%q, pg) = %q, want %q", tt.field, got, tt.wantPG)
			}
			if got := columnExpr(tt.field, sl); got != tt.wantSQL {
				t.Errorf("columnExpr(%q, sl) = %q, want %q", tt.field, got, tt.wantSQL)
			}
		})
	}
}

// TestPlaceholderList verifies the placeholder list helper.
func TestPlaceholderList(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		start   int
		n       int
		want    string
	}{
		{"postgres 1..3", PostgresDialect{}, 1, 3, "$1, $2, $3"},
		{"postgres 4..5", PostgresDialect{}, 4, 2, "$4, $5"},
		{"sqlite 1..3", SQLiteDialect{}, 1, 3, "?, ?, ?"},
		{"sqlite 10..11", SQLiteDialect{}, 10, 2, "?, ?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := placeholderList(tt.dialect, tt.start, tt.n)
			// Normalise whitespace.
			got = strings.TrimSpace(got)
			want := strings.TrimSpace(tt.want)
			if got != want {
				t.Errorf("placeholderList = %q, want %q", got, want)
			}
		})
	}
}

// TestTranslateFilter_NestedLogical verifies multi-level logical nesting.
func TestTranslateFilter_NestedLogical(t *testing.T) {
	pg := PostgresDialect{}

	node := &query.FilterNode{
		Operator: query.OpAnd,
		Children: []*query.FilterNode{
			{
				Operator: query.OpOr,
				Children: []*query.FilterNode{
					{Field: "status", Operator: query.OpEq, Value: "active"},
					{Field: "status", Operator: query.OpEq, Value: "pending"},
				},
			},
			{Field: "age", Operator: query.OpGte, Value: 18},
		},
	}

	clause, args := TranslateFilter(node, pg)

	wantClause := "((data->>'status' = $1) OR (data->>'status' = $2)) AND (data->>'age' >= $3)"
	if clause != wantClause {
		t.Errorf("clause:\n  got  %q\n  want %q", clause, wantClause)
	}

	wantArgs := []any{"active", "pending", 18}
	if len(args) != len(wantArgs) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(wantArgs))
	}
	for i := range args {
		if args[i] != wantArgs[i] {
			t.Errorf("args[%d] = %v, want %v", i, args[i], wantArgs[i])
		}
	}
}
