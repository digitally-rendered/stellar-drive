package port

import "context"

// WebhookRegistration describes a single outbound webhook endpoint. It is
// stored and retrieved by WebhookDispatcher implementations.
type WebhookRegistration struct {
	// ID is the unique identifier assigned to this registration.
	ID string `json:"id"`

	// URL is the HTTPS endpoint that receives POST deliveries.
	URL string `json:"url"`

	// Secret is used to compute an HMAC signature added to each delivery so
	// that the receiver can verify authenticity.
	Secret string `json:"secret"`

	// Events is the list of event type strings this registration listens to
	// (e.g. "post_create", "post_update"). An empty slice means all events.
	Events []string `json:"events"`

	// SchemaName scopes this registration to a specific schema. An empty
	// string means the registration applies to all schemas.
	SchemaName string `json:"schema_name,omitempty"`

	// Active controls whether deliveries are attempted. Registrations can be
	// suspended without being removed.
	Active bool `json:"active"`
}

// WebhookDispatcher is the secondary port for outbound webhook delivery.
// Implementations manage registration persistence and HTTP delivery.
type WebhookDispatcher interface {
	// Register stores the webhook registration and makes it eligible for
	// future deliveries.
	Register(ctx context.Context, reg *WebhookRegistration) error

	// Unregister removes the registration identified by id. It is not an
	// error to unregister an ID that does not exist.
	Unregister(ctx context.Context, id string) error

	// Dispatch finds all active registrations that match eventType and
	// schemaName, then delivers payload to each matching endpoint. Delivery
	// errors for individual endpoints must not prevent delivery to others.
	Dispatch(ctx context.Context, eventType string, schemaName string, payload []byte) error

	// ListRegistrations returns all stored registrations, active or not.
	ListRegistrations(ctx context.Context) ([]*WebhookRegistration, error)
}
