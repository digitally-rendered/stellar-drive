package streaming

import (
	"context"
	"log/slog"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// NATSStreamer implements port.EventStreamer for NATS (and NATS JetStream for
// durable, ordered delivery).
//
// Real implementation note: replace the stub bodies with calls to
// github.com/nats-io/nats.go. Add the dependency to go.mod before
// uncommenting the real code. JetStream is recommended for durable delivery
// guarantees equivalent to Kafka's offset-based consumers.
type NATSStreamer struct {
	url string
}

// NewNATSStreamer returns a NATSStreamer targeting the given NATS server URL
// (e.g. "nats://localhost:4222").
func NewNATSStreamer(url string) *NATSStreamer {
	return &NATSStreamer{url: url}
}

// Publish logs the message details. A real implementation would call
// nc.Publish(msg.Topic, msg.Payload) or js.Publish(msg.Topic, msg.Payload)
// for JetStream subjects.
func (n *NATSStreamer) Publish(ctx context.Context, msg *port.StreamMessage) error {
	slog.Default().InfoContext(ctx, "nats: publish",
		"subject", msg.Topic,
		"key", msg.Key,
		"payload_bytes", len(msg.Payload),
	)
	// TODO: real implementation
	// nc, err := nats.Connect(n.url)
	// if err != nil { return err }
	// return nc.Publish(msg.Topic, msg.Payload)
	return nil
}

// Subscribe logs the subscription request. A real implementation would call
// nc.Subscribe(topic, ...) or js.Subscribe(topic, ...) for JetStream
// consumers.
func (n *NATSStreamer) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg *port.StreamMessage) error) error {
	slog.Default().InfoContext(ctx, "nats: subscribe",
		"subject", topic,
		"url", n.url,
	)
	// TODO: real implementation
	// nc, err := nats.Connect(n.url)
	// if err != nil { return err }
	// _, err = nc.Subscribe(topic, func(m *nats.Msg) {
	//     _ = handler(ctx, &port.StreamMessage{Topic: topic, Payload: m.Data})
	// })
	// return err
	return nil
}

// Close is a no-op in the stub. A real implementation would drain and close
// the NATS connection.
func (n *NATSStreamer) Close() error {
	slog.Default().Info("nats: close")
	// TODO: real implementation — nc.Drain() then nc.Close()
	return nil
}
