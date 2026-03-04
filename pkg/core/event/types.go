package event

import "time"

// Type identifies a lifecycle event.
type Type string

const (
	PreCreate      Type = "pre_create"
	PostCreate     Type = "post_create"
	PreGet         Type = "pre_get"
	PostGet        Type = "post_get"
	PreList        Type = "pre_list"
	PostList       Type = "post_list"
	PreUpdate      Type = "pre_update"
	PostUpdate     Type = "post_update"
	PreDelete      Type = "pre_delete"
	PostDelete     Type = "post_delete"
	PreBulkCreate  Type = "pre_bulk_create"
	PostBulkCreate Type = "post_bulk_create"
	PreBulkUpdate  Type = "pre_bulk_update"
	PostBulkUpdate Type = "post_bulk_update"
	PreBulkDelete  Type = "pre_bulk_delete"
	PostBulkDelete Type = "post_bulk_delete"
)

// AllTypes returns all 16 lifecycle event types in definition order.
func AllTypes() []Type {
	return []Type{
		PreCreate,
		PostCreate,
		PreGet,
		PostGet,
		PreList,
		PostList,
		PreUpdate,
		PostUpdate,
		PreDelete,
		PostDelete,
		PreBulkCreate,
		PostBulkCreate,
		PreBulkUpdate,
		PostBulkUpdate,
		PreBulkDelete,
		PostBulkDelete,
	}
}

// IsPre reports whether this is a pre-operation event. Pre-event handler
// errors abort the operation; post-event handler errors are logged only.
func (t Type) IsPre() bool {
	switch t {
	case PreCreate, PreGet, PreList, PreUpdate, PreDelete,
		PreBulkCreate, PreBulkUpdate, PreBulkDelete:
		return true
	}
	return false
}

// Event is the payload passed to every Handler invocation.
//
// For pre-events, Input carries the raw caller-supplied data (create payload,
// update patch, query object, list of IDs, etc.). Result is nil.
//
// For post-events, both Input and Result are populated; Result holds the value
// produced by the operation (a *model.Document, *model.ListResult, etc.).
//
// Metadata carries ambient context such as a request ID or authenticated user
// and is never nil — the Bus initialises it to an empty map if the caller
// passes nil.
type Event struct {
	Type       Type
	SchemaName string
	Timestamp  time.Time
	Input      any            // create input, update patch, query, ids, etc.
	Result     any            // only populated for post-events
	Metadata   map[string]any // request-scoped ambient data (never nil)
}
