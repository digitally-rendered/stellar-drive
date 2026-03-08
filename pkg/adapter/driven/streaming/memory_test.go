package streaming

import (
	"context"
	"testing"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

func TestInMemoryStreamer_Publish(t *testing.T) {
	s := NewInMemoryStreamer()
	ctx := context.Background()

	msg := &port.StreamMessage{
		Topic:   "test.created",
		Key:     "entity-1",
		Payload: []byte(`{"event":"create"}`),
	}

	if err := s.Publish(ctx, msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := s.Len(); got != 1 {
		t.Fatalf("Len: got %d, want 1", got)
	}

	msgs := s.Messages()
	if msgs[0].Topic != "test.created" {
		t.Errorf("Topic: got %q, want %q", msgs[0].Topic, "test.created")
	}
	if msgs[0].Key != "entity-1" {
		t.Errorf("Key: got %q, want %q", msgs[0].Key, "entity-1")
	}
}

func TestInMemoryStreamer_Reset(t *testing.T) {
	s := NewInMemoryStreamer()
	ctx := context.Background()

	_ = s.Publish(ctx, &port.StreamMessage{Topic: "t", Payload: []byte("x")})
	_ = s.Publish(ctx, &port.StreamMessage{Topic: "t", Payload: []byte("y")})

	if got := s.Len(); got != 2 {
		t.Fatalf("before reset: got %d, want 2", got)
	}

	s.Reset()

	if got := s.Len(); got != 0 {
		t.Fatalf("after reset: got %d, want 0", got)
	}
}

func TestInMemoryStreamer_Close(t *testing.T) {
	s := NewInMemoryStreamer()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
