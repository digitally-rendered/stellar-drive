package streaming

import (
	"context"
	"sync"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// InMemoryStreamer is an in-process EventStreamer that stores published messages
// in a slice for testing and benchmarking. It is goroutine-safe.
type InMemoryStreamer struct {
	mu       sync.RWMutex
	messages []*port.StreamMessage
}

// NewInMemoryStreamer returns an InMemoryStreamer ready for use.
func NewInMemoryStreamer() *InMemoryStreamer {
	return &InMemoryStreamer{}
}

// Publish appends a copy of msg to the in-memory message list.
func (s *InMemoryStreamer) Publish(_ context.Context, msg *port.StreamMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	return nil
}

// Subscribe starts a no-op consumer (messages are only produced via Publish).
// The handler is never called because there is no external source. This method
// exists solely to satisfy the EventStreamer interface.
func (s *InMemoryStreamer) Subscribe(_ context.Context, _ string, _ func(context.Context, *port.StreamMessage) error) error {
	return nil
}

// Close is a no-op.
func (s *InMemoryStreamer) Close() error {
	return nil
}

// Messages returns a snapshot of all published messages.
func (s *InMemoryStreamer) Messages() []*port.StreamMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*port.StreamMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

// Len returns the number of published messages.
func (s *InMemoryStreamer) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// Reset clears all stored messages.
func (s *InMemoryStreamer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = s.messages[:0]
}

// PublishN publishes n messages with sequential keys for throughput testing.
func (s *InMemoryStreamer) PublishN(ctx context.Context, topic string, n int) (time.Duration, error) {
	payload := []byte(`{"event":"benchmark","n":0}`)

	start := time.Now()
	for i := 0; i < n; i++ {
		msg := &port.StreamMessage{
			Topic:   topic,
			Key:     "",
			Payload: payload,
		}
		if err := s.Publish(ctx, msg); err != nil {
			return time.Since(start), err
		}
	}
	return time.Since(start), nil
}
