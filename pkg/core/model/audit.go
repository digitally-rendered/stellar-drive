package model

import "time"

// AuditEntry records a single CRUD operation for the audit trail.
type AuditEntry struct {
	ID         string         `json:"id" bson:"_id"`
	Timestamp  time.Time      `json:"timestamp" bson:"timestamp"`
	Operation  string         `json:"operation" bson:"operation"`
	SchemaName string         `json:"schema_name" bson:"schema_name"`
	EntityID   string         `json:"entity_id,omitempty" bson:"entity_id,omitempty"`
	UserID     string         `json:"user_id,omitempty" bson:"user_id,omitempty"`
	Channel    string         `json:"channel,omitempty" bson:"channel,omitempty"`
	Changes    map[string]any `json:"changes,omitempty" bson:"changes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty" bson:"metadata,omitempty"`
}
