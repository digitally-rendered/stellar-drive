package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectData(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]any
		fields []string
		want   map[string]any
	}{
		{
			name:   "empty fields returns original",
			data:   map[string]any{"name": "Fido", "age": 3},
			fields: nil,
			want:   map[string]any{"name": "Fido", "age": 3},
		},
		{
			name:   "specified fields are extracted",
			data:   map[string]any{"name": "Fido", "age": 3, "breed": "Lab"},
			fields: []string{"name", "breed"},
			want:   map[string]any{"name": "Fido", "breed": "Lab"},
		},
		{
			name:   "non-existent fields are omitted",
			data:   map[string]any{"name": "Fido"},
			fields: []string{"name", "missing"},
			want:   map[string]any{"name": "Fido"},
		},
		{
			name:   "nil data returns nil",
			data:   nil,
			fields: []string{"name"},
			want:   nil,
		},
		{
			name:   "empty fields slice returns original",
			data:   map[string]any{"name": "Fido"},
			fields: []string{},
			want:   map[string]any{"name": "Fido"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProjectData(tt.data, tt.fields)
			assert.Equal(t, tt.want, got)
		})
	}
}
