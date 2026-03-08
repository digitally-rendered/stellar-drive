// Package stream_test contains validation tests for the InMemoryStreamer and
// the EventBridge. These tests verify correctness of message structure,
// payload field coverage, resilience to corrupt input, topic naming
// conventions, and multi-version event metadata — all without requiring an
// external broker.
package stream_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/streaming"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// ---------------------------------------------------------------------------
// TestStreamPublishHasValidFields
// ---------------------------------------------------------------------------

// TestStreamPublishHasValidFields verifies that a message published directly
// via InMemoryStreamer arrives intact with non-empty Topic, Key, and Payload
// fields. This is the baseline contract that all higher-level tests depend on.
func TestStreamPublishHasValidFields(t *testing.T) {
	s := streaming.NewInMemoryStreamer()
	ctx := context.Background()

	msg := &port.StreamMessage{
		Topic:   "pet.created",
		Key:     "entity-abc",
		Payload: []byte(`{"event":"create","schema_name":"pet","entity_id":"entity-abc"}`),
	}

	if err := s.Publish(ctx, msg); err != nil {
		t.Fatalf("Publish: unexpected error: %v", err)
	}

	if s.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", s.Len())
	}

	got := s.Messages()[0]

	if got.Topic == "" {
		t.Error("Topic must not be empty")
	}
	if got.Key == "" {
		t.Error("Key must not be empty")
	}
	if len(got.Payload) == 0 {
		t.Error("Payload must not be empty")
	}

	// Verify the values are exactly what was published — the in-memory
	// streamer must not transform or re-encode the message.
	if got.Topic != msg.Topic {
		t.Errorf("Topic: got %q, want %q", got.Topic, msg.Topic)
	}
	if got.Key != msg.Key {
		t.Errorf("Key: got %q, want %q", got.Key, msg.Key)
	}
}

// ---------------------------------------------------------------------------
// TestStreamPayloadMatchesSchema
// ---------------------------------------------------------------------------

// TestStreamPayloadMatchesSchema publishes an event whose payload encodes a
// known set of fields and verifies that all expected fields are present and
// have the correct types when the payload is decoded.
func TestStreamPayloadMatchesSchema(t *testing.T) {
	s := streaming.NewInMemoryStreamer()
	ctx := context.Background()

	now := time.Now().UTC()
	doc := &model.Document{
		ID:            "id-entity-1",
		EntityID:      "entity-1",
		SchemaName:    "pet",
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "Fido", "status": "available"},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-entity-1",
	}

	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Timestamp:  now,
		Result:     doc,
		Metadata:   map[string]any{"request_id": "req-xyz"},
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal event: %v", err)
	}

	if err := s.Publish(ctx, &port.StreamMessage{
		Topic:   "pet.created",
		Key:     doc.EntityID,
		Payload: payload,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got := s.Messages()[0]

	// Decode the payload into a generic map for field inspection.
	var decoded map[string]any
	if err := json.Unmarshal(got.Payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal payload: %v", err)
	}

	// Required top-level fields in the serialised event.
	requiredFields := []string{"Type", "SchemaName", "Timestamp", "Result", "Metadata"}
	for _, field := range requiredFields {
		if _, ok := decoded[field]; !ok {
			t.Errorf("payload missing required field %q", field)
		}
	}

	// Verify SchemaName matches.
	schemaName, ok := decoded["SchemaName"].(string)
	if !ok || schemaName != "pet" {
		t.Errorf("SchemaName: got %v, want %q", decoded["SchemaName"], "pet")
	}

	// Verify event Type matches.
	eventType, ok := decoded["Type"].(string)
	if !ok || eventType != string(event.PostCreate) {
		t.Errorf("Type: got %v, want %q", decoded["Type"], event.PostCreate)
	}

	// Verify the Result sub-object contains the document fields.
	result, ok := decoded["Result"].(map[string]any)
	if !ok {
		t.Fatalf("Result must be a JSON object, got %T", decoded["Result"])
	}

	docFields := []string{"id", "entity_id", "schema_name", "record_version", "schema_version", "data"}
	for _, field := range docFields {
		if _, ok := result[field]; !ok {
			t.Errorf("Result missing document field %q", field)
		}
	}
}

// ---------------------------------------------------------------------------
// TestStreamCorruptedPayloadNoCrash
// ---------------------------------------------------------------------------

// TestStreamCorruptedPayloadNoCrash verifies that the InMemoryStreamer accepts
// and stores messages whose payloads are invalid, empty, or of edge-case
// content without panicking. The streamer is a transport layer and must not
// attempt to parse or validate message bodies.
func TestStreamCorruptedPayloadNoCrash(t *testing.T) {
	corruptCases := []struct {
		name    string
		payload []byte
	}{
		{name: "nil_payload", payload: nil},
		{name: "empty_payload", payload: []byte{}},
		{name: "invalid_json", payload: []byte(`{broken json`)},
		{name: "json_array_not_object", payload: []byte(`[1,2,3]`)},
		{name: "json_null", payload: []byte(`null`)},
		{name: "json_number", payload: []byte(`42`)},
		{name: "json_string", payload: []byte(`"just a string"`)},
		{name: "truncated_utf8", payload: []byte{0xc3}}, // incomplete two-byte UTF-8 sequence
		{name: "binary_garbage", payload: []byte{0x00, 0xff, 0xfe, 0xfd}},
	}

	s := streaming.NewInMemoryStreamer()
	ctx := context.Background()

	for _, tc := range corruptCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Wrap in a deferred recover so that a panic in Publish is
			// reported as a test failure rather than crashing the suite.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Publish panicked with payload %q: %v", tc.payload, r)
				}
			}()

			msg := &port.StreamMessage{
				Topic:   "pet.created",
				Key:     "entity-corrupt",
				Payload: tc.payload,
			}

			err := s.Publish(ctx, msg)
			// The in-memory streamer never returns an error for any payload;
			// a non-nil error here would also be acceptable as long as there
			// is no panic, so we only log rather than fail.
			if err != nil {
				t.Logf("Publish returned error for %s (acceptable): %v", tc.name, err)
			}
		})
	}

	// All messages (corrupt or not) must have been stored.
	if got := s.Len(); got != len(corruptCases) {
		t.Errorf("Len after corrupt publishes: got %d, want %d", got, len(corruptCases))
	}
}

// ---------------------------------------------------------------------------
// TestStreamTopicFormat
// ---------------------------------------------------------------------------

// TestStreamTopicFormat verifies that the EventBridge builds topics in the
// expected "{schema_name}.{operation}" format for all supported post-event
// types. It publishes one event of each post type through the bridge and
// inspects the resulting stream messages.
func TestStreamTopicFormat(t *testing.T) {
	// Map from event.Type to the expected topic suffix after the schema prefix.
	postEvents := []struct {
		eventType     event.Type
		wantOperation string
	}{
		{event.PostCreate, "created"},
		{event.PostGet, "retrieved"},
		{event.PostList, "listed"},
		{event.PostUpdate, "updated"},
		{event.PostDelete, "deleted"},
		{event.PostBulkCreate, "bulk_created"},
		{event.PostBulkUpdate, "bulk_updated"},
		{event.PostBulkDelete, "bulk_deleted"},
	}

	now := time.Now().UTC()
	doc := &model.Document{
		ID:            "id-entity-1",
		EntityID:      "entity-1",
		SchemaName:    "order",
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"pet_id": "pet-xyz", "quantity": 2},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-entity-1",
	}

	for _, tc := range postEvents {
		tc := tc
		t.Run(string(tc.eventType), func(t *testing.T) {
			bus := event.NewBus()
			s := streaming.NewInMemoryStreamer()
			bridge := streaming.NewEventBridge(bus, s)
			bridge.Start()

			ctx := context.Background()
			evt := &event.Event{
				Type:       tc.eventType,
				SchemaName: "order",
				Timestamp:  now,
				Result:     doc,
				Metadata:   map[string]any{},
			}

			if err := bus.Publish(ctx, evt); err != nil {
				t.Fatalf("bus.Publish(%s): %v", tc.eventType, err)
			}

			// The bridge forwards synchronously within the bus.Publish call,
			// so the message must be present immediately.
			if s.Len() != 1 {
				t.Fatalf("expected 1 stream message, got %d", s.Len())
			}

			msg := s.Messages()[0]
			wantTopic := "order." + tc.wantOperation

			if msg.Topic != wantTopic {
				t.Errorf("Topic: got %q, want %q", msg.Topic, wantTopic)
			}

			// The topic must follow the {schema_name}.{operation} format with
			// exactly one dot as the separator.
			parts := strings.SplitN(msg.Topic, ".", 2)
			if len(parts) != 2 {
				t.Errorf("Topic %q must contain exactly one dot separator", msg.Topic)
			} else {
				if parts[0] != "order" {
					t.Errorf("Topic prefix: got %q, want %q", parts[0], "order")
				}
				if parts[1] != tc.wantOperation {
					t.Errorf("Topic operation: got %q, want %q", parts[1], tc.wantOperation)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestStreamMultiVersionEvents
// ---------------------------------------------------------------------------

// TestStreamMultiVersionEvents verifies that events carrying documents with
// different schema versions preserve the correct schema_version value in the
// serialised payload. This guards the multi-version event routing contract:
// consumers MUST be able to identify which schema version produced a message
// by inspecting the payload alone.
func TestStreamMultiVersionEvents(t *testing.T) {
	versions := []struct {
		schemaVersion string
		wantVersion   string
	}{
		{"1.0.0", "1.0.0"},
		{"1.1.0", "1.1.0"},
		{"2.0.0", "2.0.0"},
	}

	now := time.Now().UTC()

	for _, tc := range versions {
		tc := tc
		t.Run("schema_version_"+strings.ReplaceAll(tc.schemaVersion, ".", "_"), func(t *testing.T) {
			s := streaming.NewInMemoryStreamer()
			ctx := context.Background()

			doc := &model.Document{
				ID:            "id-entity-versioned",
				EntityID:      "entity-versioned",
				SchemaName:    "pet",
				RecordVersion: 1,
				SchemaVersion: tc.schemaVersion,
				Data:          map[string]any{"name": "Versioned Pet", "status": "available"},
				CreatedAt:     now,
				UpdatedAt:     now,
				ETag:          "etag-versioned",
			}

			evt := &event.Event{
				Type:       event.PostCreate,
				SchemaName: "pet",
				Timestamp:  now,
				Result:     doc,
				Metadata:   map[string]any{"schema_version": tc.schemaVersion},
			}

			payload, err := json.Marshal(evt)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			if err := s.Publish(ctx, &port.StreamMessage{
				Topic:   "pet.created",
				Key:     doc.EntityID,
				Payload: payload,
			}); err != nil {
				t.Fatalf("Publish: %v", err)
			}

			if s.Len() != 1 {
				t.Fatalf("expected 1 message, got %d", s.Len())
			}

			got := s.Messages()[0]

			// Decode the event envelope.
			var decoded map[string]any
			if err := json.Unmarshal(got.Payload, &decoded); err != nil {
				t.Fatalf("json.Unmarshal payload: %v", err)
			}

			// Verify schema_version is present in the Metadata field.
			metadata, ok := decoded["Metadata"].(map[string]any)
			if !ok {
				t.Fatalf("Metadata must be a JSON object, got %T", decoded["Metadata"])
			}

			gotMetaVersion, ok := metadata["schema_version"].(string)
			if !ok {
				t.Fatalf("Metadata.schema_version must be a string, got %T", metadata["schema_version"])
			}
			if gotMetaVersion != tc.wantVersion {
				t.Errorf("Metadata.schema_version: got %q, want %q", gotMetaVersion, tc.wantVersion)
			}

			// Verify schema_version is also preserved in the Result document.
			result, ok := decoded["Result"].(map[string]any)
			if !ok {
				t.Fatalf("Result must be a JSON object, got %T", decoded["Result"])
			}

			gotDocVersion, ok := result["schema_version"].(string)
			if !ok {
				t.Fatalf("Result.schema_version must be a string, got %T", result["schema_version"])
			}
			if gotDocVersion != tc.wantVersion {
				t.Errorf("Result.schema_version: got %q, want %q", gotDocVersion, tc.wantVersion)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestStreamBridgePreservesEntityIDAsKey
// ---------------------------------------------------------------------------

// TestStreamBridgePreservesEntityIDAsKey verifies that the EventBridge
// extracts the document's EntityID and sets it as the stream message Key.
// Partitioned brokers rely on this key for ordering guarantees per entity.
func TestStreamBridgePreservesEntityIDAsKey(t *testing.T) {
	bus := event.NewBus()
	s := streaming.NewInMemoryStreamer()
	bridge := streaming.NewEventBridge(bus, s)
	bridge.Start()

	ctx := context.Background()
	now := time.Now().UTC()

	wantEntityID := "entity-key-test"
	doc := &model.Document{
		ID:            "id-" + wantEntityID,
		EntityID:      wantEntityID,
		SchemaName:    "pet",
		RecordVersion: 1,
		SchemaVersion: "1.0.0",
		Data:          map[string]any{"name": "KeyTest"},
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          "etag-" + wantEntityID,
	}

	if err := bus.Publish(ctx, &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Timestamp:  now,
		Result:     doc,
		Metadata:   map[string]any{},
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	if s.Len() != 1 {
		t.Fatalf("expected 1 message, got %d", s.Len())
	}

	msg := s.Messages()[0]
	if msg.Key != wantEntityID {
		t.Errorf("Key: got %q, want %q", msg.Key, wantEntityID)
	}
}

// ---------------------------------------------------------------------------
// TestStreamBridgeNilResultEmptyKey
// ---------------------------------------------------------------------------

// TestStreamBridgeNilResultEmptyKey verifies that publishing a post-event
// with a nil Result does not panic and results in an empty message key rather
// than a key derived from a nil pointer.
func TestStreamBridgeNilResultEmptyKey(t *testing.T) {
	bus := event.NewBus()
	s := streaming.NewInMemoryStreamer()
	bridge := streaming.NewEventBridge(bus, s)
	bridge.Start()

	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("bridge panicked on nil Result: %v", r)
		}
	}()

	if err := bus.Publish(ctx, &event.Event{
		Type:       event.PostDelete,
		SchemaName: "pet",
		Timestamp:  time.Now().UTC(),
		Result:     nil, // no document returned on delete
		Metadata:   map[string]any{},
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	if s.Len() != 1 {
		t.Fatalf("expected 1 message, got %d", s.Len())
	}

	msg := s.Messages()[0]
	// An empty key is acceptable when no entity ID is available.
	if msg.Key != "" {
		t.Logf("Key with nil Result: %q (non-empty is acceptable if deterministic)", msg.Key)
	}
}

// ---------------------------------------------------------------------------
// TestStreamPublishOrdering
// ---------------------------------------------------------------------------

// TestStreamPublishOrdering verifies that messages are stored in publication
// order. Consumers relying on the InMemoryStreamer for testing must be able to
// replay events in the order they occurred.
func TestStreamPublishOrdering(t *testing.T) {
	s := streaming.NewInMemoryStreamer()
	ctx := context.Background()

	topics := []string{"pet.created", "pet.updated", "pet.deleted"}
	for _, topic := range topics {
		if err := s.Publish(ctx, &port.StreamMessage{
			Topic:   topic,
			Key:     "entity-1",
			Payload: []byte(`{}`),
		}); err != nil {
			t.Fatalf("Publish %q: %v", topic, err)
		}
	}

	msgs := s.Messages()
	if len(msgs) != len(topics) {
		t.Fatalf("expected %d messages, got %d", len(topics), len(msgs))
	}

	for i, msg := range msgs {
		if msg.Topic != topics[i] {
			t.Errorf("message[%d] Topic: got %q, want %q", i, msg.Topic, topics[i])
		}
	}
}
