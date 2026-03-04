package schema

import "time"

// SchemaEnvelope wraps a JSON Schema with metadata. Version lives here, not in URLs.
type SchemaEnvelope struct {
	Name        string         `json:"name" bson:"name"`
	Version     string         `json:"version" bson:"version"`
	Description string         `json:"description,omitempty" bson:"description,omitempty"`
	Storage     string         `json:"storage,omitempty" bson:"storage,omitempty"`
	Collection  string         `json:"collection,omitempty" bson:"collection,omitempty"`
	Schema      map[string]any `json:"schema" bson:"schema"`
	Indexes     []IndexDef     `json:"indexes,omitempty" bson:"indexes,omitempty"`
	Extensions  map[string]any `json:"extensions,omitempty" bson:"extensions,omitempty"`
	CreatedAt   time.Time      `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" bson:"updated_at"`
	Active      bool           `json:"active" bson:"active"`
}

// IndexDef describes a storage index for the schema's collection.
type IndexDef struct {
	Fields []string `json:"fields" bson:"fields"`
	Unique bool     `json:"unique" bson:"unique"`
	Sparse bool     `json:"sparse,omitempty" bson:"sparse,omitempty"`
}

// SchemaDefinition is the parsed, indexed representation of a schema.
type SchemaDefinition struct {
	Name           string
	Version        string
	Description    string
	RawSchema      map[string]any
	Fields         []FieldDefinition
	RequiredFields []string
	Indexes        []IndexDef
	Storage        string // "mongo" or "sql"
	Collection     string
	Extensions     map[string]any
}

// FieldDefinition describes a single field parsed from a JSON Schema property.
type FieldDefinition struct {
	Name        string           // property name
	JSONType    string           // "string", "number", "integer", "boolean", "array", "object"
	GoType      string           // resolved Go type
	Format      string           // "date-time", "email", "uri", etc.
	Required    bool
	Default     any
	Enum        []any
	Ref         string            // $ref target
	Items       *FieldDefinition  // for arrays
	Properties  []FieldDefinition // for nested objects
	Extensions  map[string]any    // x-stellar-* extensions
}

// ModelVariant describes which variant of the model is being generated.
type ModelVariant int

const (
	VariantDocument ModelVariant = iota
	VariantCreate
	VariantUpdate
)

// SchemaEvent is emitted when schemas change (for distributed sync).
type SchemaEvent struct {
	Type       SchemaEventType
	Envelope   *SchemaEnvelope
	OccurredAt time.Time
}

// SchemaEventType classifies the kind of schema change.
type SchemaEventType string

const (
	SchemaRegistered  SchemaEventType = "schema_registered"
	SchemaUpdated     SchemaEventType = "schema_updated"
	SchemaDeactivated SchemaEventType = "schema_deactivated"
)
