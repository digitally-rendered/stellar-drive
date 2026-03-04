package event_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
)

// makeEvent is a helper that returns a minimal Event for the given type and
// schema name so individual tests stay concise.
func makeEvent(t event.Type, schemaName string) *event.Event {
	return &event.Event{
		Type:       t,
		SchemaName: schemaName,
		Timestamp:  time.Now(),
	}
}

// recorder builds a Handler that appends a caller-supplied label to calls.
func recorder(calls *[]string, label string) event.Handler {
	return func(_ context.Context, _ *event.Event) error {
		*calls = append(*calls, label)
		return nil
	}
}

// errHandler builds a Handler that always returns the supplied error.
func errHandler(err error) event.Handler {
	return func(_ context.Context, _ *event.Event) error {
		return err
	}
}

// -----------------------------------------------------------------------
// Global handler fires for any schema
// -----------------------------------------------------------------------

func TestBus_GlobalHandlerFiresForAnySchema(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.Subscribe(event.PreCreate, "", recorder(&calls, "global"))

	schemas := []string{"pets", "users", "orders"}
	for _, s := range schemas {
		calls = nil
		err := b.Publish(context.Background(), makeEvent(event.PreCreate, s))
		require.NoError(t, err, "schema=%s", s)
		assert.Equal(t, []string{"global"}, calls, "global handler should fire for schema %q", s)
	}
}

// -----------------------------------------------------------------------
// Schema-specific handler only fires for its schema
// -----------------------------------------------------------------------

func TestBus_SchemaSpecificHandlerOnlyFiresForItsSchema(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.Subscribe(event.PreCreate, "pets", recorder(&calls, "pets-handler"))

	tests := []struct {
		schema   string
		wantFire bool
	}{
		{"pets", true},
		{"users", false},
		{"orders", false},
	}

	for _, tc := range tests {
		calls = nil
		err := b.Publish(context.Background(), makeEvent(event.PreCreate, tc.schema))
		require.NoError(t, err, "schema=%s", tc.schema)
		if tc.wantFire {
			assert.Equal(t, []string{"pets-handler"}, calls,
				"handler should fire for schema %q", tc.schema)
		} else {
			assert.Empty(t, calls,
				"handler should NOT fire for schema %q", tc.schema)
		}
	}
}

// -----------------------------------------------------------------------
// Pre-event error aborts (Publish returns the error)
// -----------------------------------------------------------------------

func TestBus_PreEventErrorAborts(t *testing.T) {
	b := event.NewBus()

	sentinel := errors.New("pre-event rejected")
	var afterCalls []string

	b.Subscribe(event.PreCreate, "", errHandler(sentinel))
	b.Subscribe(event.PreCreate, "", recorder(&afterCalls, "after-error"))

	err := b.Publish(context.Background(), makeEvent(event.PreCreate, "pets"))

	require.Error(t, err, "Publish should propagate a pre-event handler error")
	assert.ErrorIs(t, err, sentinel, "error chain should contain the sentinel")
	assert.Empty(t, afterCalls,
		"handler registered after the failing one must not be called")
}

// -----------------------------------------------------------------------
// Post-event error does not abort (Publish returns nil)
// -----------------------------------------------------------------------

func TestBus_PostEventErrorDoesNotAbort(t *testing.T) {
	b := event.NewBus()

	sentinel := errors.New("post-event failed")
	var afterCalls []string

	b.Subscribe(event.PostCreate, "", errHandler(sentinel))
	b.Subscribe(event.PostCreate, "", recorder(&afterCalls, "after-error"))

	err := b.Publish(context.Background(), makeEvent(event.PostCreate, "pets"))

	assert.NoError(t, err, "Publish must return nil even when a post-event handler errors")
	assert.Equal(t, []string{"after-error"}, afterCalls,
		"subsequent handlers must still be called after a post-event error")
}

// -----------------------------------------------------------------------
// Subscribe ordering: global before schema-specific
// -----------------------------------------------------------------------

func TestBus_GlobalHandlerFiresBeforeSchemaSpecific(t *testing.T) {
	b := event.NewBus()

	var calls []string
	// Intentionally subscribe the schema-specific handler first to prove that
	// ordering is not driven by subscription order but by scope.
	b.Subscribe(event.PreCreate, "pets", recorder(&calls, "specific"))
	b.Subscribe(event.PreCreate, "", recorder(&calls, "global"))

	err := b.Publish(context.Background(), makeEvent(event.PreCreate, "pets"))
	require.NoError(t, err)

	assert.Equal(t, []string{"global", "specific"}, calls,
		"global handler must be invoked before schema-specific handler")
}

// -----------------------------------------------------------------------
// Additional edge-case: multiple globals fire in subscription order
// -----------------------------------------------------------------------

func TestBus_MultipleGlobalsFireInSubscriptionOrder(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.Subscribe(event.PreUpdate, "", recorder(&calls, "g1"))
	b.Subscribe(event.PreUpdate, "", recorder(&calls, "g2"))
	b.Subscribe(event.PreUpdate, "", recorder(&calls, "g3"))

	err := b.Publish(context.Background(), makeEvent(event.PreUpdate, "users"))
	require.NoError(t, err)

	assert.Equal(t, []string{"g1", "g2", "g3"}, calls)
}

// -----------------------------------------------------------------------
// Additional edge-case: handler not called for different event type
// -----------------------------------------------------------------------

func TestBus_HandlerNotCalledForDifferentEventType(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.Subscribe(event.PreCreate, "", recorder(&calls, "create-handler"))

	err := b.Publish(context.Background(), makeEvent(event.PreUpdate, "pets"))
	require.NoError(t, err)

	assert.Empty(t, calls, "handler registered for PreCreate must not fire for PreUpdate")
}

// -----------------------------------------------------------------------
// SubscribeAll registers handler for every event type
// -----------------------------------------------------------------------

func TestBus_SubscribeAll(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.SubscribeAll("", recorder(&calls, "all"))

	allTypes := event.AllTypes()
	for _, et := range allTypes {
		calls = nil
		evt := makeEvent(et, "pets")
		_ = b.Publish(context.Background(), evt)
		assert.Len(t, calls, 1, "SubscribeAll handler should fire once for event type %s", et)
	}
}

// -----------------------------------------------------------------------
// Clear removes all handlers
// -----------------------------------------------------------------------

func TestBus_ClearRemovesAllHandlers(t *testing.T) {
	b := event.NewBus()

	var calls []string
	b.Subscribe(event.PreCreate, "", recorder(&calls, "handler"))
	b.Clear()

	err := b.Publish(context.Background(), makeEvent(event.PreCreate, "pets"))
	require.NoError(t, err)

	assert.Empty(t, calls, "no handlers should fire after Clear")
}

// -----------------------------------------------------------------------
// Metadata is initialised to non-nil map when caller passes nil
// -----------------------------------------------------------------------

func TestBus_MetadataInitialisedWhenNil(t *testing.T) {
	b := event.NewBus()

	var captured *event.Event
	b.Subscribe(event.PreCreate, "", func(_ context.Context, e *event.Event) error {
		captured = e
		return nil
	})

	evt := &event.Event{
		Type:       event.PreCreate,
		SchemaName: "pets",
		Timestamp:  time.Now(),
		// Metadata intentionally left nil
	}

	err := b.Publish(context.Background(), evt)
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.NotNil(t, captured.Metadata, "Bus must initialise Metadata to a non-nil map")
}

// -----------------------------------------------------------------------
// AllTypes returns exactly 16 distinct entries
// -----------------------------------------------------------------------

func TestAllTypes_Returns16DistinctTypes(t *testing.T) {
	all := event.AllTypes()
	assert.Len(t, all, 16)

	seen := make(map[event.Type]struct{}, len(all))
	for _, et := range all {
		seen[et] = struct{}{}
	}
	assert.Len(t, seen, 16, "AllTypes must not contain duplicates")
}

// -----------------------------------------------------------------------
// IsPre correctly classifies all types
// -----------------------------------------------------------------------

func TestType_IsPre(t *testing.T) {
	tests := []struct {
		t      event.Type
		wantPre bool
	}{
		{event.PreCreate, true},
		{event.PostCreate, false},
		{event.PreGet, true},
		{event.PostGet, false},
		{event.PreList, true},
		{event.PostList, false},
		{event.PreUpdate, true},
		{event.PostUpdate, false},
		{event.PreDelete, true},
		{event.PostDelete, false},
		{event.PreBulkCreate, true},
		{event.PostBulkCreate, false},
		{event.PreBulkUpdate, true},
		{event.PostBulkUpdate, false},
		{event.PreBulkDelete, true},
		{event.PostBulkDelete, false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.t), func(t *testing.T) {
			assert.Equal(t, tc.wantPre, tc.t.IsPre())
		})
	}
}
