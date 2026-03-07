package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// AuditTrail records CRUD operations as audit log entries by subscribing to
// EventBus lifecycle events. Construct with NewAuditTrail and register with
// Register.
type AuditTrail struct {
	store      port.AuditStore
	trackReads bool
}

// AuditOption configures an AuditTrail.
type AuditOption func(*AuditTrail)

// WithTrackReads enables auditing of read operations (get and list).
func WithTrackReads(track bool) AuditOption {
	return func(a *AuditTrail) {
		a.trackReads = track
	}
}

// NewAuditTrail constructs an AuditTrail that persists entries via the given
// AuditStore.
func NewAuditTrail(store port.AuditStore, opts ...AuditOption) *AuditTrail {
	a := &AuditTrail{store: store}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Register subscribes the audit trail to the appropriate events on bus. Global
// (schema="") handlers fire for every schema.
func (a *AuditTrail) Register(bus *event.Bus) {
	bus.Subscribe(event.PostCreate, "", a.onWrite("create"))
	bus.Subscribe(event.PostUpdate, "", a.onWrite("update"))
	bus.Subscribe(event.PostDelete, "", a.onWrite("delete"))
	bus.Subscribe(event.PostBulkCreate, "", a.onWrite("bulk_create"))
	bus.Subscribe(event.PostBulkUpdate, "", a.onWrite("bulk_update"))
	bus.Subscribe(event.PostBulkDelete, "", a.onWrite("bulk_delete"))

	if a.trackReads {
		bus.Subscribe(event.PostGet, "", a.onWrite("get"))
		bus.Subscribe(event.PostList, "", a.onWrite("list"))
	}
}

// onWrite returns an event handler that records an audit entry for the given
// operation name.
func (a *AuditTrail) onWrite(operation string) event.Handler {
	return func(ctx context.Context, evt *event.Event) error {
		entry := &model.AuditEntry{
			ID:         uuid.New().String(),
			Timestamp:  time.Now().UTC(),
			Operation:  operation,
			SchemaName: evt.SchemaName,
			Metadata:   evt.Metadata,
		}

		// Extract entity_id from result when available.
		if doc, ok := evt.Result.(*model.Document); ok && doc != nil {
			entry.EntityID = doc.EntityID
		}

		// Extract user_id from event metadata.
		if userID, ok := evt.Metadata["user_id"].(string); ok {
			entry.UserID = userID
		}

		// Extract channel from event metadata.
		if channel, ok := evt.Metadata["channel"].(string); ok {
			entry.Channel = channel
		}

		if err := a.store.Record(ctx, entry); err != nil {
			slog.ErrorContext(ctx, "audit trail: failed to record entry",
				"operation", operation,
				"schema", evt.SchemaName,
				"error", err,
			)
			// Return nil so post-event processing continues.
			return nil
		}

		return nil
	}
}
