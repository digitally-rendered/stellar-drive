// Package streaming provides EventStreamer adapters for external message
// brokers. The implementations in this package are intentionally stubbed: they
// log publish and subscribe actions via slog so that the rest of the system
// can be developed and tested without broker infrastructure. Each stub carries
// a comment marking where the real broker client calls should be inserted once
// the corresponding dependency is added to go.mod.
package streaming

import (
	"context"
	"log/slog"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// KafkaStreamer implements port.EventStreamer for Apache Kafka.
//
// Real implementation note: replace the stub bodies with calls to
// github.com/segmentio/kafka-go (or github.com/twmb/franz-go for a
// lower-level alternative). Add the dependency to go.mod before uncommenting
// the real code.
type KafkaStreamer struct {
	brokers []string
}

// NewKafkaStreamer returns a KafkaStreamer that targets the given broker
// addresses (e.g. ["localhost:9092"]).
func NewKafkaStreamer(brokers []string) *KafkaStreamer {
	addrs := make([]string, len(brokers))
	copy(addrs, brokers)
	return &KafkaStreamer{brokers: addrs}
}

// Publish logs the message details. A real implementation would use a
// kafka-go writer to produce the message to msg.Topic.
func (k *KafkaStreamer) Publish(ctx context.Context, msg *port.StreamMessage) error {
	slog.Default().InfoContext(ctx, "kafka: publish",
		"topic", msg.Topic,
		"key", msg.Key,
		"payload_bytes", len(msg.Payload),
	)
	// TODO: real implementation
	// writer := kafka.NewWriter(kafka.WriterConfig{Brokers: k.brokers, Topic: msg.Topic})
	// return writer.WriteMessages(ctx, kafka.Message{Key: []byte(msg.Key), Value: msg.Payload})
	return nil
}

// Subscribe logs the subscription request. A real implementation would use a
// kafka-go reader to consume messages from topic and call handler for each.
func (k *KafkaStreamer) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg *port.StreamMessage) error) error {
	slog.Default().InfoContext(ctx, "kafka: subscribe",
		"topic", topic,
		"brokers", k.brokers,
	)
	// TODO: real implementation
	// reader := kafka.NewReader(kafka.ReaderConfig{Brokers: k.brokers, Topic: topic, GroupID: "stellar-drive"})
	// go func() {
	//     for {
	//         m, err := reader.ReadMessage(ctx)
	//         if err != nil { return }
	//         _ = handler(ctx, &port.StreamMessage{Topic: topic, Key: string(m.Key), Payload: m.Value})
	//     }
	// }()
	return nil
}

// Close is a no-op in the stub. A real implementation would close all open
// readers and writers.
func (k *KafkaStreamer) Close() error {
	slog.Default().Info("kafka: close")
	// TODO: real implementation — close all managed readers/writers
	return nil
}
