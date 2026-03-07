package sdtest

import (
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// DataGenerator produces deterministic test payloads from a SchemaDefinition.
// Values are based solely on field type and format so that tests remain
// readable and reproducible without random seeds.
//
//	gen := sdtest.NewDataGenerator()
//	payload := gen.GenerateCreate(def)
type DataGenerator struct{}

// NewDataGenerator returns a DataGenerator ready for use.
func NewDataGenerator() *DataGenerator {
	return &DataGenerator{}
}

// GenerateCreate produces a create payload containing a value for every field
// defined in def. Required fields are always included; optional fields are
// included as well so callers have a complete, schema-valid document to POST.
func (g *DataGenerator) GenerateCreate(def *schema.SchemaDefinition) map[string]any {
	if def == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(def.Fields))
	for _, field := range def.Fields {
		out[field.Name] = valueFor(field)
	}
	return out
}

// GenerateUpdate produces a partial update payload. It includes only the first
// field found in def (if any) so that the patch is intentionally minimal. This
// is useful for confirming that PATCH endpoints accept partial bodies.
//
// If def has no fields, an empty map is returned.
func (g *DataGenerator) GenerateUpdate(def *schema.SchemaDefinition) map[string]any {
	if def == nil || len(def.Fields) == 0 {
		return map[string]any{}
	}
	// Use the first field for a deterministic, minimal update payload.
	field := def.Fields[0]
	return map[string]any{
		field.Name: valueFor(field),
	}
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// valueFor returns a deterministic test value for a FieldDefinition based on
// its JSONType and Format.
func valueFor(field schema.FieldDefinition) any {
	switch field.JSONType {
	case "string":
		if field.Format == "date-time" {
			return time.Now().UTC().Format(time.RFC3339)
		}
		return "test-" + field.Name

	case "integer":
		return 42

	case "number":
		return 3.14

	case "boolean":
		return true

	case "array":
		return []any{}

	case "object":
		return map[string]any{}

	default:
		// Includes "null" and any unrecognised type; use a string sentinel so
		// the field is at least present and serialisable.
		return "test-" + field.Name
	}
}
