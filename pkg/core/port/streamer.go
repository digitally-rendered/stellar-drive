package port

import "context"

// StreamMessage is the unit of data exchanged over an event stream. It is
// intentionally transport-agnostic so that Kafka, NATS, Pulsar, and in-process
// implementations can all satisfy EventStreamer.
type StreamMessage struct {
	// Topic is the named channel on which the message is published or from
	// which it was received.
	Topic string

	// Key is used by partitioned brokers to route related messages to the same
	// partition (e.g. the entity ID).
	Key string

	// Payload is the raw, serialised message body (typically JSON).
	Payload []byte

	// Headers carries optional broker-level or application-level metadata
	// (e.g. content-type, trace-id).
	Headers map[string]string
}

// EventStreamer is the secondary port for durable, ordered event streaming.
// Implementations bridge stellar-drive lifecycle events to an external
// message broker.
type EventStreamer interface {
	// Publish sends msg to the broker. The call blocks until the broker
	// acknowledges the message or ctx is cancelled.
	Publish(ctx context.Context, msg *StreamMessage) error

	// Subscribe registers handler as a consumer of topic. The handler is
	// called once per received message; returning a non-nil error signals a
	// processing failure whose handling is implementation-defined (e.g. NACK,
	// dead-letter, retry).
	//
	// Subscribe runs the consumer loop in the background and returns once the
	// subscription is established. The loop stops when ctx is cancelled.
	Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg *StreamMessage) error) error

	// Close gracefully shuts down all producers and consumers managed by this
	// streamer and releases any held resources.
	Close() error
}
