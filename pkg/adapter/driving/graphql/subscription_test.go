package graphql

import (
	"context"
	"testing"
	"time"

	gql "github.com/graphql-go/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	schemapkg "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// stubBus is a minimal port.EventBus implementation for tests. It records
// every Subscribe call so tests can fire handlers directly.
type stubBus struct {
	subscriptions []stubSubscription
}

type stubSubscription struct {
	eventType  event.Type
	schemaName string
	handler    event.Handler
}

func (b *stubBus) Subscribe(eventType event.Type, schemaName string, handler event.Handler) {
	b.subscriptions = append(b.subscriptions, stubSubscription{
		eventType:  eventType,
		schemaName: schemaName,
		handler:    handler,
	})
}

func (b *stubBus) Publish(ctx context.Context, evt *event.Event) error {
	for _, s := range b.subscriptions {
		if s.eventType == evt.Type && (s.schemaName == "" || s.schemaName == evt.SchemaName) {
			if err := s.handler(ctx, evt); err != nil {
				return err
			}
		}
	}
	return nil
}

// sampleDocument returns a minimal model.Document for testing.
func sampleDocument(entityID string) *model.Document {
	now := time.Now().UTC()
	return &model.Document{
		ID:            "mongo-" + entityID,
		EntityID:      entityID,
		SchemaName:    "pet",
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "Fido", "status": "available"},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "abc123",
	}
}

// objectTypesFixture builds a minimal objectTypes map for use in
// buildSubscriptionFields.
func objectTypesFixture() map[string]*gql.Object {
	return map[string]*gql.Object{
		"pet": gql.NewObject(gql.ObjectConfig{
			Name: "Pet",
			Fields: gql.Fields{
				"entity_id": &gql.Field{Type: gql.String},
			},
		}),
	}
}

// --------------------------------------------------------------------------
// Tests: buildSubscriptionFields
// --------------------------------------------------------------------------

func TestBuildSubscriptionFields_NilBus(t *testing.T) {
	fields := buildSubscriptionFields(nil, nil, nil)
	assert.Empty(t, fields, "nil bus should produce no subscription fields")
}

func TestBuildSubscriptionFields_NoDefinitions(t *testing.T) {
	bus := &stubBus{}
	fields := buildSubscriptionFields(nil, objectTypesFixture(), bus)
	assert.Empty(t, fields, "no definitions should produce no subscription fields")
}

func TestBuildSubscriptionFields_NamesAndTypes(t *testing.T) {
	bus := &stubBus{}
	objTypes := objectTypesFixture()

	// Build a minimal SchemaDefinition slice using only exported fields.
	defs := minimalSchemaDefs()

	fields := buildSubscriptionFields(defs, objTypes, bus)

	wantKeys := []string{
		"onPetCreated",
		"onPetUpdated",
		"onPetDeleted",
	}
	for _, k := range wantKeys {
		assert.Contains(t, fields, k, "expected subscription field %q", k)
	}
	assert.Len(t, fields, len(wantKeys))
}

func TestBuildSubscriptionFields_SkipsUnknownObjectType(t *testing.T) {
	bus := &stubBus{}
	// Pass an empty objectTypes map — every schema should be skipped.
	fields := buildSubscriptionFields(minimalSchemaDefs(), map[string]*gql.Object{}, bus)
	assert.Empty(t, fields)
}

// --------------------------------------------------------------------------
// Tests: makeSubscriptionResolver
// --------------------------------------------------------------------------

func TestMakeSubscriptionResolver_ReturnsChannel(t *testing.T) {
	bus := &stubBus{}
	resolver := makeSubscriptionResolver("pet", event.PostCreate, bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := resolver(gql.ResolveParams{Context: ctx})
	require.NoError(t, err)

	ch, ok := result.(chan any)
	require.True(t, ok, "resolver must return chan any")
	assert.NotNil(t, ch)
}

func TestMakeSubscriptionResolver_RegistersHandlerOnBus(t *testing.T) {
	bus := &stubBus{}
	resolver := makeSubscriptionResolver("pet", event.PostCreate, bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := resolver(gql.ResolveParams{Context: ctx})
	require.NoError(t, err)

	require.Len(t, bus.subscriptions, 1)
	assert.Equal(t, event.PostCreate, bus.subscriptions[0].eventType)
	assert.Equal(t, "pet", bus.subscriptions[0].schemaName)
}

func TestMakeSubscriptionResolver_DeliversDocumentOnEvent(t *testing.T) {
	bus := &stubBus{}
	resolver := makeSubscriptionResolver("pet", event.PostCreate, bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := resolver(gql.ResolveParams{Context: ctx})
	require.NoError(t, err)

	ch := result.(chan any)

	doc := sampleDocument("ent-1")
	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Result:     doc,
		Metadata:   map[string]any{},
	}

	require.NoError(t, bus.Publish(ctx, evt))

	select {
	case msg := <-ch:
		flat, ok := msg.(map[string]any)
		require.True(t, ok, "channel value must be map[string]any")
		assert.Equal(t, "ent-1", flat["entity_id"])
		assert.Equal(t, "Fido", flat["name"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")
	}
}

func TestMakeSubscriptionResolver_ChannelClosedOnContextCancel(t *testing.T) {
	bus := &stubBus{}
	resolver := makeSubscriptionResolver("pet", event.PostCreate, bus)

	ctx, cancel := context.WithCancel(context.Background())

	result, err := resolver(gql.ResolveParams{Context: ctx})
	require.NoError(t, err)

	ch := result.(chan any)

	cancel() // trigger context cancellation

	// The goroutine monitoring ctx.Done() should close the channel shortly.
	select {
	case _, open := <-ch:
		assert.False(t, open, "channel must be closed after context cancellation")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}
}

func TestMakeSubscriptionResolver_IgnoresNonDocumentResults(t *testing.T) {
	bus := &stubBus{}
	resolver := makeSubscriptionResolver("pet", event.PostCreate, bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := resolver(gql.ResolveParams{Context: ctx})
	require.NoError(t, err)

	ch := result.(chan any)

	// Publish an event whose Result is not a *model.Document.
	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Result:     "not-a-document",
		Metadata:   map[string]any{},
	}
	require.NoError(t, bus.Publish(ctx, evt))

	// Channel should remain empty — no panic, no delivery.
	select {
	case <-ch:
		t.Fatal("channel must not receive value for non-document result")
	case <-time.After(50 * time.Millisecond):
		// Expected: nothing delivered.
	}
}

func TestMakeSubscriptionResolver_MultipleSubscriptions(t *testing.T) {
	cases := []struct {
		name      string
		eventType event.Type
	}{
		{"created", event.PostCreate},
		{"updated", event.PostUpdate},
		{"deleted", event.PostDelete},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := &stubBus{}
			resolver := makeSubscriptionResolver("pet", tc.eventType, bus)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			result, err := resolver(gql.ResolveParams{Context: ctx})
			require.NoError(t, err)

			ch := result.(chan any)
			doc := sampleDocument("ent-x")

			require.NoError(t, bus.Publish(ctx, &event.Event{
				Type:       tc.eventType,
				SchemaName: "pet",
				Result:     doc,
				Metadata:   map[string]any{},
			}))

			select {
			case msg := <-ch:
				flat := msg.(map[string]any)
				assert.Equal(t, "ent-x", flat["entity_id"])
			case <-time.After(time.Second):
				t.Fatalf("timed out for event type %s", tc.eventType)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Helpers used by multiple tests
// --------------------------------------------------------------------------

// minimalSchemaDefs returns a single-element slice with a "pet" schema
// definition that has no user-defined fields (just the name is needed for
// subscription field generation).
func minimalSchemaDefs() []*schemapkg.SchemaDefinition {
	return []*schemapkg.SchemaDefinition{
		{
			Name:        "pet",
			Version:     "1.0.0",
			Description: "A pet in the petstore.",
		},
	}
}
