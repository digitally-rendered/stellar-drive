package streaming

import (
	"context"
	"log/slog"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// RedisStreamer implements port.EventStreamer using Redis Streams (XADD/XREAD).
//
// Real implementation note: replace the stub bodies with calls to
// github.com/redis/go-redis/v9. Add the dependency to go.mod before
// uncommenting the real code. Consumer groups (XGROUP/XREADGROUP) are
// recommended for at-least-once delivery with multiple consumers.
type RedisStreamer struct {
	addr string
}

// NewRedisStreamer returns a RedisStreamer targeting the given Redis address
// (e.g. "localhost:6379").
func NewRedisStreamer(addr string) *RedisStreamer {
	return &RedisStreamer{addr: addr}
}

// Publish logs the message details. A real implementation would call
// client.XAdd to append the message to the named Redis stream.
func (r *RedisStreamer) Publish(ctx context.Context, msg *port.StreamMessage) error {
	slog.Default().InfoContext(ctx, "redis: publish",
		"stream", msg.Topic,
		"key", msg.Key,
		"payload_bytes", len(msg.Payload),
	)
	// TODO: real implementation
	// client := redis.NewClient(&redis.Options{Addr: r.addr})
	// return client.XAdd(ctx, &redis.XAddArgs{
	//     Stream: msg.Topic,
	//     Values: map[string]any{"key": msg.Key, "payload": msg.Payload},
	// }).Err()
	return nil
}

// Subscribe logs the subscription request. A real implementation would call
// client.XRead or client.XReadGroup in a background goroutine and invoke
// handler for each received entry.
func (r *RedisStreamer) Subscribe(ctx context.Context, topic string, handler func(ctx context.Context, msg *port.StreamMessage) error) error {
	slog.Default().InfoContext(ctx, "redis: subscribe",
		"stream", topic,
		"addr", r.addr,
	)
	// TODO: real implementation
	// client := redis.NewClient(&redis.Options{Addr: r.addr})
	// go func() {
	//     for {
	//         msgs, err := client.XRead(ctx, &redis.XReadArgs{Streams: []string{topic, "$"}, Block: 0}).Result()
	//         if err != nil { return }
	//         for _, m := range msgs[0].Messages {
	//             payload, _ := m.Values["payload"].(string)
	//             key, _ := m.Values["key"].(string)
	//             _ = handler(ctx, &port.StreamMessage{Topic: topic, Key: key, Payload: []byte(payload)})
	//         }
	//     }
	// }()
	return nil
}

// Close is a no-op in the stub. A real implementation would close the Redis
// client connection.
func (r *RedisStreamer) Close() error {
	slog.Default().Info("redis: close")
	// TODO: real implementation — client.Close()
	return nil
}
