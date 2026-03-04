package streaming

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// EventBridge subscribes to post-events on the EventBus and forwards each one
// to an EventStreamer. It provides a one-way bridge from the in-process
// lifecycle bus to an external message broker.
//
// Topic format: "{schema_name}.{operation}" where operation is the bare verb
// extracted from the post-event type (e.g. "post_create" → "created",
// "post_bulk_update" → "bulk_updated").
//
// Message key: the entity_id extracted from the post-event Result when it
// holds a *model.Document; empty string otherwise.
type EventBridge struct {
	streamer port.EventStreamer
	bus      port.EventBus
}

// NewEventBridge wires bus and streamer together. Call Start to begin
// forwarding events.
func NewEventBridge(bus port.EventBus, streamer port.EventStreamer) *EventBridge {
	return &EventBridge{
		streamer: streamer,
		bus:      bus,
	}
}

// Start subscribes to every post-event type on the global scope (schemaName="")
// and publishes matching events to the streamer. Start returns immediately;
// the subscriptions remain active until the bus or streamer is closed.
func (b *EventBridge) Start() {
	postEvents := []event.Type{
		event.PostCreate,
		event.PostGet,
		event.PostList,
		event.PostUpdate,
		event.PostDelete,
		event.PostBulkCreate,
		event.PostBulkUpdate,
		event.PostBulkDelete,
	}

	for _, et := range postEvents {
		et := et // capture for closure
		b.bus.Subscribe(et, "", func(ctx context.Context, evt *event.Event) error {
			b.forward(ctx, evt)
			return nil
		})
	}
}

// forward serialises evt and publishes it to the streamer. Errors are logged
// and not propagated so that a failing streamer never aborts a post-event.
func (b *EventBridge) forward(ctx context.Context, evt *event.Event) {
	topic := buildTopic(evt.SchemaName, string(evt.Type))
	key := extractEntityID(evt.Result)

	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Default().ErrorContext(ctx, "event bridge: marshal event",
			"schema", evt.SchemaName,
			"event_type", string(evt.Type),
			"error", err,
		)
		return
	}

	msg := &port.StreamMessage{
		Topic:   topic,
		Key:     key,
		Payload: payload,
	}
	if err := b.streamer.Publish(ctx, msg); err != nil {
		slog.Default().ErrorContext(ctx, "event bridge: publish to stream",
			"topic", topic,
			"key", key,
			"error", err,
		)
	}
}

// buildTopic converts a schema name and raw event type string into a topic in
// the form "{schema_name}.{operation}". Examples:
//
//	("pet", "post_create")      → "pet.created"
//	("user", "post_bulk_update") → "user.bulk_updated"
func buildTopic(schemaName, eventType string) string {
	op := operationFromEventType(eventType)
	return schemaName + "." + op
}

// operationFromEventType strips the "post_" prefix and converts the remainder
// to a past-tense verb (e.g. "post_create" → "created").
func operationFromEventType(eventType string) string {
	// Strip "post_" prefix.
	verb := strings.TrimPrefix(eventType, "post_")
	// Map bare verbs to past-tense forms.
	switch verb {
	case "create":
		return "created"
	case "get":
		return "retrieved"
	case "list":
		return "listed"
	case "update":
		return "updated"
	case "delete":
		return "deleted"
	case "bulk_create":
		return "bulk_created"
	case "bulk_update":
		return "bulk_updated"
	case "bulk_delete":
		return "bulk_deleted"
	default:
		return verb
	}
}

// extractEntityID attempts to pull an entity_id from the event Result. It
// handles *model.Document and *model.ListResult (uses the first item's
// entity_id). Returns an empty string if extraction fails.
func extractEntityID(result any) string {
	if result == nil {
		return ""
	}
	switch v := result.(type) {
	case *model.Document:
		return v.EntityID
	case *model.ListResult:
		if len(v.Items) > 0 {
			return v.Items[0].EntityID
		}
	}
	return ""
}
