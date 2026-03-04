package jsonutil_test

import (
	"testing"

	"github.com/digitally-rendered/stellar-drive/internal/jsonutil"
	"github.com/stretchr/testify/assert"
)

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name  string
		base  map[string]any
		patch map[string]any
		want  map[string]any
	}{
		{
			name:  "nil base and patch",
			base:  nil,
			patch: nil,
			want:  map[string]any{},
		},
		{
			name:  "patch into empty base",
			base:  map[string]any{},
			patch: map[string]any{"a": 1},
			want:  map[string]any{"a": 1},
		},
		{
			name:  "empty patch leaves base unchanged",
			base:  map[string]any{"a": 1},
			patch: map[string]any{},
			want:  map[string]any{"a": 1},
		},
		{
			name:  "scalar patch overwrites scalar base",
			base:  map[string]any{"a": 1, "b": "old"},
			patch: map[string]any{"b": "new"},
			want:  map[string]any{"a": 1, "b": "new"},
		},
		{
			name: "nested maps are merged recursively",
			base: map[string]any{
				"user": map[string]any{"name": "alice", "age": 30},
			},
			patch: map[string]any{
				"user": map[string]any{"age": 31},
			},
			want: map[string]any{
				"user": map[string]any{"name": "alice", "age": 31},
			},
		},
		{
			name: "nil value in patch deletes key",
			base: map[string]any{"a": 1, "b": 2},
			patch: map[string]any{
				"b": nil,
			},
			want: map[string]any{"a": 1},
		},
		{
			name: "nil value deletes nested key",
			base: map[string]any{
				"cfg": map[string]any{"timeout": 30, "retries": 3},
			},
			patch: map[string]any{
				"cfg": map[string]any{"retries": nil},
			},
			want: map[string]any{
				"cfg": map[string]any{"timeout": 30},
			},
		},
		{
			name: "array in patch replaces array in base",
			base: map[string]any{
				"tags": []any{"go", "grpc"},
			},
			patch: map[string]any{
				"tags": []any{"rest"},
			},
			want: map[string]any{
				"tags": []any{"rest"},
			},
		},
		{
			name: "patch map overwrites non-map base value",
			base: map[string]any{"x": 42},
			patch: map[string]any{
				"x": map[string]any{"nested": true},
			},
			want: map[string]any{
				"x": map[string]any{"nested": true},
			},
		},
		{
			name: "patch non-map overwrites map base value",
			base: map[string]any{
				"x": map[string]any{"nested": true},
			},
			patch: map[string]any{"x": 42},
			want:  map[string]any{"x": 42},
		},
		{
			name: "deep three-level merge",
			base: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": 1,
						"d": 2,
					},
				},
			},
			patch: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"d": 99,
					},
				},
			},
			want: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": 1,
						"d": 99,
					},
				},
			},
		},
		{
			name: "base is not mutated",
			base: map[string]any{"k": "original"},
			patch: map[string]any{"k": "changed"},
			want: map[string]any{"k": "changed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Snapshot the base value for mutation check.
			var origBaseVal any
			if tc.base != nil {
				origBaseVal = tc.base["k"]
			}

			got := jsonutil.DeepMerge(tc.base, tc.patch)
			assert.Equal(t, tc.want, got)

			// Verify base was not mutated for the mutation test case.
			if tc.name == "base is not mutated" {
				assert.Equal(t, origBaseVal, tc.base["k"], "base map must not be mutated")
			}
		})
	}
}
