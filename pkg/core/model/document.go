package model

import "time"

// Document represents a versioned record with audit fields.
// Every write creates a new Document with an incremented RecordVersion.
type Document struct {
	ID            string         `json:"id" bson:"_id"`
	EntityID      string         `json:"entity_id" bson:"entity_id"`
	SchemaName    string         `json:"schema_name" bson:"schema_name"`
	RecordVersion int            `json:"record_version" bson:"record_version"`
	SchemaVersion string         `json:"schema_version" bson:"schema_version"`
	Data          map[string]any `json:"data" bson:"data"`
	CreatedAt     time.Time      `json:"created_at" bson:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at" bson:"updated_at"`
	DeletedAt     *time.Time     `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	CreatedBy     string         `json:"created_by,omitempty" bson:"created_by,omitempty"`
	UpdatedBy     string         `json:"updated_by,omitempty" bson:"updated_by,omitempty"`
	DeletedBy     string         `json:"deleted_by,omitempty" bson:"deleted_by,omitempty"`
	ETag          string         `json:"etag" bson:"etag"`
}

// IsTombstone returns true if this document version is a soft-delete marker.
func (d *Document) IsTombstone() bool { return d.DeletedAt != nil }

// BulkUpdateItem pairs an entity ID with partial update data.
type BulkUpdateItem struct {
	EntityID string         `json:"entity_id"`
	Data     map[string]any `json:"data"`
}

// ListResult holds paginated query results.
type ListResult struct {
	Items   []*Document `json:"items"`
	Total   int64       `json:"total"`
	HasMore bool        `json:"has_more"`
	Cursor  string      `json:"cursor,omitempty"`
}
