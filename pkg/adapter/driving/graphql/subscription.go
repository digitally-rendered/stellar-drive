package graphql

import (
	"context"
	"log/slog"

	gql "github.com/graphql-go/graphql"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	schemapkg "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

const subscriptionChannelBuffer = 16

// buildSubscriptionFields creates three subscription fields per schema:
//
//   - on{Name}Created — fires after a successful create operation
//   - on{Name}Updated — fires after a successful update operation
//   - on{Name}Deleted — fires after a successful delete operation
//
// Each field returns the corresponding GraphQL Object type. The resolver
// subscribes to the event bus for the appropriate (eventType, schemaName)
// pair and forwards flattened documents onto a buffered channel.
//
// bus may be nil; in that case an empty Fields map is returned so callers
// need not guard against a nil bus before calling.
func buildSubscriptionFields(
	defs []*schemapkg.SchemaDefinition,
	objectTypes map[string]*gql.Object,
	bus port.EventBus,
) gql.Fields {
	if bus == nil {
		return gql.Fields{}
	}

	fields := gql.Fields{}

	for _, def := range defs {
		objType, ok := objectTypes[def.Name]
		if !ok {
			// No object type means the schema had no wired service; skip it.
			continue
		}

		pascal := toPascalCase(def.Name)

		// on{Name}Created
		fields["on"+pascal+"Created"] = &gql.Field{
			Type:        objType,
			Description: "Subscribe to newly created " + def.Name + " records.",
			Resolve:     makeSubscriptionResolver(def.Name, event.PostCreate, bus),
		}

		// on{Name}Updated
		fields["on"+pascal+"Updated"] = &gql.Field{
			Type:        objType,
			Description: "Subscribe to updates on " + def.Name + " records.",
			Resolve:     makeSubscriptionResolver(def.Name, event.PostUpdate, bus),
		}

		// on{Name}Deleted
		fields["on"+pascal+"Deleted"] = &gql.Field{
			Type:        objType,
			Description: "Subscribe to soft-deletions of " + def.Name + " records.",
			Resolve:     makeSubscriptionResolver(def.Name, event.PostDelete, bus),
		}
	}

	return fields
}

// makeSubscriptionResolver returns a FieldResolveFn that, when called by the
// graphql-go executor, subscribes to the event bus for (eventType, schemaName)
// and returns a buffered channel of flattened document maps.
//
// The graphql-go library drives subscriptions by expecting the resolver to
// return a chan any. The executor reads from the channel and resolves each
// emitted value against the field's sub-selection set.
//
// Lifecycle:
//  1. The resolver creates a buffered channel and registers an event handler.
//  2. The handler converts each event's Result (*model.Document) into a
//     flattened map and sends it on the channel without blocking the Publish
//     caller. Events arriving faster than the subscriber consumes them are
//     dropped with a warning log.
//  3. A background goroutine monitors p.Context cancellation and closes the
//     channel, signalling the executor to tear down the subscription.
func makeSubscriptionResolver(schemaName string, eventType event.Type, bus port.EventBus) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		ch := make(chan any, subscriptionChannelBuffer)

		// handler is registered once per resolver invocation. It is invoked by
		// the event bus in the goroutine that calls Publish, so it must not
		// block and must be safe for concurrent use.
		handler := func(ctx context.Context, evt *event.Event) error {
			doc, ok := evt.Result.(*model.Document)
			if !ok || doc == nil {
				// Result is not a document (e.g. a bulk event or unexpected
				// type). Skip silently — do not break the subscription.
				return nil
			}

			flat := flattenDocument(doc)

			select {
			case ch <- flat:
				// Delivered successfully.
			default:
				// Channel full: the consumer is too slow. Drop and warn rather
				// than blocking the entire event bus dispatch.
				slog.Default().WarnContext(ctx,
					"graphql subscription channel full; event dropped",
					"schema", schemaName,
					"event_type", string(eventType),
				)
			}
			return nil
		}

		bus.Subscribe(eventType, schemaName, handler)

		// Background goroutine: close the channel when the subscription context
		// is cancelled. The graphql-go executor treats a closed channel as the
		// end of the subscription stream.
		go func() {
			<-p.Context.Done()
			close(ch)
		}()

		return ch, nil
	}
}
