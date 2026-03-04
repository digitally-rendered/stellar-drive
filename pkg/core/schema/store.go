package schema

import "context"

// Store is the port interface for persisting and distributing schema envelopes.
// Implementations live in pkg/adapter/driven/*.
type Store interface {
	// Save persists or updates a schema envelope.
	Save(ctx context.Context, envelope *SchemaEnvelope) error

	// Get retrieves a specific schema envelope by name and version.
	// If version is empty, the implementation should return the latest version.
	Get(ctx context.Context, name string, version string) (*SchemaEnvelope, error)

	// List returns all schema envelopes (latest version per name).
	List(ctx context.Context) ([]*SchemaEnvelope, error)

	// ListVersions returns all versions of a named schema.
	ListVersions(ctx context.Context, name string) ([]*SchemaEnvelope, error)

	// Delete removes all versions of a named schema.
	Delete(ctx context.Context, name string) error

	// Watch returns a channel that receives SchemaEvents as schemas are
	// created, updated, or deactivated. The channel is closed when ctx is done.
	Watch(ctx context.Context) (<-chan SchemaEvent, error)
}
