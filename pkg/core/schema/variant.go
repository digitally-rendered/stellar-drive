package schema

import "time"

// DocumentFields returns all fields for the Document variant:
// user-defined fields plus the standard audit fields.
func DocumentFields(def *SchemaDefinition) []FieldDefinition {
	fields := make([]FieldDefinition, len(def.Fields))
	copy(fields, def.Fields)
	return append(fields, AuditFields()...)
}

// CreateFields returns fields for the Create variant: only user-defined fields
// (both required and optional). No audit fields are included because the
// storage layer generates them on insert.
func CreateFields(def *SchemaDefinition) []FieldDefinition {
	fields := make([]FieldDefinition, len(def.Fields))
	copy(fields, def.Fields)
	return fields
}

// UpdateFields returns fields for the Update variant with PATCH semantics:
// all user-defined fields are made optional so callers can supply only the
// subset they wish to change.
func UpdateFields(def *SchemaDefinition) []FieldDefinition {
	fields := make([]FieldDefinition, len(def.Fields))
	for i, f := range def.Fields {
		f.Required = false
		fields[i] = f
	}
	return fields
}

// AuditFields returns the standard audit field definitions that are appended
// to every Document variant.
func AuditFields() []FieldDefinition {
	trueVal := true
	_ = trueVal // used via pointer below

	return []FieldDefinition{
		{
			Name:     "entity_id",
			JSONType: "string",
			GoType:   "string",
			Required: true,
		},
		{
			Name:     "record_version",
			JSONType: "integer",
			GoType:   "int64",
			Required: true,
		},
		{
			Name:     "schema_version",
			JSONType: "string",
			GoType:   "string",
			Required: true,
		},
		{
			Name:     "schema_name",
			JSONType: "string",
			GoType:   "string",
			Required: true,
		},
		{
			Name:     "created_at",
			JSONType: "string",
			GoType:   "time.Time",
			Format:   "date-time",
			Required: true,
			Default:  time.Time{},
		},
		{
			Name:     "updated_at",
			JSONType: "string",
			GoType:   "time.Time",
			Format:   "date-time",
			Required: true,
			Default:  time.Time{},
		},
		{
			Name:     "deleted_at",
			JSONType: "string",
			GoType:   "*time.Time",
			Format:   "date-time",
			Required: false,
		},
		{
			Name:     "created_by",
			JSONType: "string",
			GoType:   "string",
			Required: false,
		},
		{
			Name:     "updated_by",
			JSONType: "string",
			GoType:   "string",
			Required: false,
		},
		{
			Name:     "deleted_by",
			JSONType: "string",
			GoType:   "string",
			Required: false,
		},
		{
			Name:     "etag",
			JSONType: "string",
			GoType:   "string",
			Required: true,
		},
	}
}
