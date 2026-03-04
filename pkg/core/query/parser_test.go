package query

import (
	"encoding/json"
	"testing"
)

// mustRaw is a test helper that marshals v to json.RawMessage.
func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustRaw: marshal: %v", err)
	}
	return b
}

// TestParse covers the main shapes that Parse must handle.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       any // marshalled to json.RawMessage
		wantErr     bool
		checkResult func(t *testing.T, node *FilterNode)
	}{
		{
			name:  "nil input",
			input: nil,
			checkResult: func(t *testing.T, node *FilterNode) {
				if node != nil {
					t.Errorf("expected nil node, got %+v", node)
				}
			},
		},
		{
			name:  "empty object",
			input: map[string]any{},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node != nil {
					t.Errorf("expected nil node for empty object, got %+v", node)
				}
			},
		},
		{
			name: "single field eq",
			input: map[string]any{
				"name": map[string]any{"_eq": "doggie"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpEq {
					t.Errorf("operator: want %q, got %q", OpEq, node.Operator)
				}
				if node.Field != "name" {
					t.Errorf("field: want %q, got %q", "name", node.Field)
				}
				if node.Value != "doggie" {
					t.Errorf("value: want %q, got %v", "doggie", node.Value)
				}
			},
		},
		{
			name: "two fields produce implicit _and",
			input: map[string]any{
				"name":   map[string]any{"_eq": "doggie"},
				"status": map[string]any{"_in": []any{"available", "pending"}},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpAnd {
					t.Errorf("operator: want %q, got %q", OpAnd, node.Operator)
				}
				if len(node.Children) != 2 {
					t.Errorf("children: want 2, got %d", len(node.Children))
				}
			},
		},
		{
			name: "explicit _and with two conditions",
			input: map[string]any{
				"_and": []any{
					map[string]any{"name": map[string]any{"_eq": "doggie"}},
					map[string]any{"age": map[string]any{"_gt": float64(2)}},
				},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpAnd {
					t.Errorf("operator: want %q, got %q", OpAnd, node.Operator)
				}
				if len(node.Children) != 2 {
					t.Errorf("children: want 2, got %d", len(node.Children))
				}
			},
		},
		{
			name: "explicit _or",
			input: map[string]any{
				"_or": []any{
					map[string]any{"status": map[string]any{"_eq": "active"}},
					map[string]any{"status": map[string]any{"_eq": "pending"}},
				},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpOr {
					t.Errorf("operator: want %q, got %q", OpOr, node.Operator)
				}
				if len(node.Children) != 2 {
					t.Errorf("children: want 2, got %d", len(node.Children))
				}
			},
		},
		{
			name: "_not wraps a single condition",
			input: map[string]any{
				"_not": map[string]any{
					"status": map[string]any{"_eq": "deleted"},
				},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpNot {
					t.Errorf("operator: want %q, got %q", OpNot, node.Operator)
				}
				if len(node.Children) != 1 {
					t.Errorf("children: want 1, got %d", len(node.Children))
				}
			},
		},
		{
			name: "_in with array value",
			input: map[string]any{
				"status": map[string]any{"_in": []any{"available", "pending", "sold"}},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpIn {
					t.Errorf("operator: want %q, got %q", OpIn, node.Operator)
				}
				vals, ok := node.Value.([]any)
				if !ok {
					t.Fatalf("value: want []any, got %T", node.Value)
				}
				if len(vals) != 3 {
					t.Errorf("value length: want 3, got %d", len(vals))
				}
			},
		},
		{
			name: "_nin excludes values",
			input: map[string]any{
				"status": map[string]any{"_nin": []any{"deleted"}},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpNin {
					t.Errorf("operator: want %q, got %q", OpNin, node.Operator)
				}
			},
		},
		{
			name: "comparison operators on numeric field",
			input: map[string]any{
				"price": map[string]any{"_gte": float64(10), "_lte": float64(100)},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				// Two conditions on the same field → implicit _and.
				if node.Operator == OpAnd {
					if len(node.Children) != 2 {
						t.Errorf("children: want 2, got %d", len(node.Children))
					}
					return
				}
				// Single operator remains a leaf.
				if node.Operator != OpGte && node.Operator != OpLte {
					t.Errorf("unexpected operator %q", node.Operator)
				}
			},
		},
		{
			name: "_exists with boolean",
			input: map[string]any{
				"email": map[string]any{"_exists": true},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpExists {
					t.Errorf("operator: want %q, got %q", OpExists, node.Operator)
				}
				if node.Value != true {
					t.Errorf("value: want true, got %v", node.Value)
				}
			},
		},
		{
			name: "_is_null check",
			input: map[string]any{
				"deleted_at": map[string]any{"_is_null": true},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpIsNull {
					t.Errorf("operator: want %q, got %q", OpIsNull, node.Operator)
				}
			},
		},
		{
			name: "string operators: _like",
			input: map[string]any{
				"name": map[string]any{"_like": "%dog%"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpLike {
					t.Errorf("operator: want %q, got %q", OpLike, node.Operator)
				}
			},
		},
		{
			name: "string operators: _ilike",
			input: map[string]any{
				"name": map[string]any{"_ilike": "%DOG%"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpILike {
					t.Errorf("operator: want %q, got %q", OpILike, node.Operator)
				}
			},
		},
		{
			name: "_startswith",
			input: map[string]any{
				"name": map[string]any{"_startswith": "dog"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpStartsWith {
					t.Errorf("operator: want %q, got %q", OpStartsWith, node.Operator)
				}
			},
		},
		{
			name: "_endswith",
			input: map[string]any{
				"name": map[string]any{"_endswith": "gie"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpEndsWith {
					t.Errorf("operator: want %q, got %q", OpEndsWith, node.Operator)
				}
			},
		},
		{
			name: "_contains",
			input: map[string]any{
				"tags": map[string]any{"_contains": "fluffy"},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpContains {
					t.Errorf("operator: want %q, got %q", OpContains, node.Operator)
				}
			},
		},
		{
			name: "nested _and inside _or",
			input: map[string]any{
				"_or": []any{
					map[string]any{
						"_and": []any{
							map[string]any{"age": map[string]any{"_gt": float64(5)}},
							map[string]any{"weight": map[string]any{"_lt": float64(20)}},
						},
					},
					map[string]any{"status": map[string]any{"_eq": "special"}},
				},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
				if node.Operator != OpOr {
					t.Errorf("operator: want %q, got %q", OpOr, node.Operator)
				}
				if len(node.Children) != 2 {
					t.Errorf("children: want 2, got %d", len(node.Children))
				}
				andNode := node.Children[0]
				if andNode.Operator != OpAnd {
					t.Errorf("nested operator: want %q, got %q", OpAnd, andNode.Operator)
				}
				if len(andNode.Children) != 2 {
					t.Errorf("nested children: want 2, got %d", len(andNode.Children))
				}
			},
		},
		{
			name:    "unknown operator returns error",
			input:   map[string]any{"name": map[string]any{"_regex": "dog.*"}},
			wantErr: true,
		},
		{
			name:    "unknown top-level key starting with _ returns error",
			input:   map[string]any{"_unknown": []any{}},
			wantErr: true,
		},
		{
			name: "deeply nested valid query at max depth",
			// 5 levels of _and nesting — should succeed.
			input: map[string]any{
				"_and": []any{
					map[string]any{
						"_and": []any{
							map[string]any{
								"_and": []any{
									map[string]any{
										"_and": []any{
											map[string]any{"status": map[string]any{"_eq": "ok"}},
										},
									},
								},
							},
						},
					},
				},
			},
			checkResult: func(t *testing.T, node *FilterNode) {
				if node == nil {
					t.Fatal("expected non-nil node")
				}
			},
		},
		{
			name: "query exceeding max nesting depth returns error",
			// 6 levels of nesting — exceeds maxParseDepth=5.
			input: map[string]any{
				"_and": []any{
					map[string]any{
						"_and": []any{
							map[string]any{
								"_and": []any{
									map[string]any{
										"_and": []any{
											map[string]any{
												"_and": []any{
													map[string]any{"status": map[string]any{"_eq": "ok"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var raw json.RawMessage
			if tc.input != nil {
				raw = mustRaw(t, tc.input)
			}

			node, err := Parse(raw)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (node=%+v)", node)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.checkResult != nil {
				tc.checkResult(t, node)
			}
		})
	}
}

// TestParseSortString covers ParseSortString edge cases.
func TestParseSortString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []SortField
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single field asc no prefix",
			input: "name",
			want:  []SortField{{Field: "name", Direction: SortAsc}},
		},
		{
			name:  "single field asc plus prefix",
			input: "+name",
			want:  []SortField{{Field: "name", Direction: SortAsc}},
		},
		{
			name:  "single field desc",
			input: "-created_at",
			want:  []SortField{{Field: "created_at", Direction: SortDesc}},
		},
		{
			name:  "multiple fields mixed directions",
			input: "+name,-created_at,updated_at",
			want: []SortField{
				{Field: "name", Direction: SortAsc},
				{Field: "created_at", Direction: SortDesc},
				{Field: "updated_at", Direction: SortAsc},
			},
		},
		{
			name:  "whitespace is trimmed",
			input: " +name , -created_at ",
			want: []SortField{
				{Field: "name", Direction: SortAsc},
				{Field: "created_at", Direction: SortDesc},
			},
		},
		{
			name:  "empty segments are skipped",
			input: "name,,created_at",
			want: []SortField{
				{Field: "name", Direction: SortAsc},
				{Field: "created_at", Direction: SortAsc},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ParseSortString(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("length: want %d, got %d (%v)", len(tc.want), len(got), got)
			}
			for i, wf := range tc.want {
				if got[i].Field != wf.Field {
					t.Errorf("[%d] field: want %q, got %q", i, wf.Field, got[i].Field)
				}
				if got[i].Direction != wf.Direction {
					t.Errorf("[%d] direction: want %q, got %q", i, wf.Direction, got[i].Direction)
				}
			}
		})
	}
}

// TestDefaultSort validates the DefaultSort sentinel.
func TestDefaultSort(t *testing.T) {
	t.Parallel()

	fields := DefaultSort()
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].Field != "created_at" {
		t.Errorf("field: want %q, got %q", "created_at", fields[0].Field)
	}
	if fields[0].Direction != SortDesc {
		t.Errorf("direction: want %q, got %q", SortDesc, fields[0].Direction)
	}
}

// TestFilterNodeIsLogical checks the IsLogical helper.
func TestFilterNodeIsLogical(t *testing.T) {
	t.Parallel()

	cases := []struct {
		op   Operator
		want bool
	}{
		{OpAnd, true},
		{OpOr, true},
		{OpNot, true},
		{OpEq, false},
		{OpIn, false},
		{OpGte, false},
		{OpExists, false},
	}
	for _, c := range cases {
		node := &FilterNode{Operator: c.op}
		if got := node.IsLogical(); got != c.want {
			t.Errorf("op=%q IsLogical(): want %v, got %v", c.op, c.want, got)
		}
	}
}

// TestIsValidOperator covers the operator allowlist.
func TestIsValidOperator(t *testing.T) {
	t.Parallel()

	valid := []Operator{
		OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte,
		OpIn, OpNin, OpLike, OpILike, OpContains,
		OpStartsWith, OpEndsWith, OpExists, OpIsNull,
		OpAnd, OpOr, OpNot,
	}
	for _, op := range valid {
		if !IsValidOperator(op) {
			t.Errorf("expected %q to be valid", op)
		}
	}

	invalid := []Operator{"_regex", "_match", "_fuzzy", "eq", ""}
	for _, op := range invalid {
		if IsValidOperator(op) {
			t.Errorf("expected %q to be invalid", op)
		}
	}
}

// BenchmarkParse measures parsing throughput for a representative query.
func BenchmarkParse(b *testing.B) {
	raw := mustRawB(b, map[string]any{
		"_and": []any{
			map[string]any{"name": map[string]any{"_like": "%dog%"}},
			map[string]any{"status": map[string]any{"_in": []any{"available", "pending"}}},
			map[string]any{"age": map[string]any{"_gte": float64(1), "_lte": float64(10)}},
		},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func mustRawB(b *testing.B, v any) json.RawMessage {
	b.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		b.Fatalf("mustRawB: %v", err)
	}
	return raw
}
