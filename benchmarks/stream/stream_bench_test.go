package stream_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/streaming"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// BenchmarkInMemoryPublish measures the latency of a single Publish call.
func BenchmarkInMemoryPublish(b *testing.B) {
	s := streaming.NewInMemoryStreamer()
	ctx := context.Background()
	msg := &port.StreamMessage{
		Topic:   "pet.created",
		Key:     "entity-1",
		Payload: []byte(`{"event":"create","schema_name":"pet"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Publish(ctx, msg)
	}
	b.ReportMetric(float64(s.Len()), "messages")
}

// BenchmarkInMemoryPublishN measures sustained throughput of N messages.
func BenchmarkInMemoryPublishN(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(formatN(n), func(b *testing.B) {
			s := streaming.NewInMemoryStreamer()
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Reset()
				dur, err := s.PublishN(ctx, "pet.created", n)
				if err != nil {
					b.Fatal(err)
				}
				_ = dur
			}
			b.ReportMetric(float64(n), "messages/op")
		})
	}
}

// BenchmarkBridgeForward measures EventBridge.forward() end-to-end:
// subscribe → emit → forward → publish to InMemoryStreamer.
func BenchmarkBridgeForward(b *testing.B) {
	bus := event.NewBus()
	s := streaming.NewInMemoryStreamer()
	bridge := streaming.NewEventBridge(bus, s)
	bridge.Start()

	ctx := context.Background()

	doc := &model.Document{
		ID:            "doc-1",
		EntityID:      "entity-1",
		SchemaName:    "pet",
		RecordVersion: 1,
		Data:          map[string]any{"name": "Fido"},
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Result:     doc,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.Publish(ctx, evt)
	}
	b.ReportMetric(float64(s.Len()), "published")
}

// BenchmarkFanout measures publishing the same event to multiple streamers.
func BenchmarkFanout(b *testing.B) {
	for _, fanout := range []int{1, 3, 5} {
		b.Run(formatN(fanout)+"_adapters", func(b *testing.B) {
			ctx := context.Background()
			streamers := make([]*streaming.InMemoryStreamer, fanout)
			for i := range streamers {
				streamers[i] = streaming.NewInMemoryStreamer()
			}

			msg := &port.StreamMessage{
				Topic:   "pet.created",
				Key:     "entity-1",
				Payload: []byte(`{"event":"create"}`),
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, s := range streamers {
					_ = s.Publish(ctx, msg)
				}
			}

			total := 0
			for _, s := range streamers {
				total += s.Len()
			}
			b.ReportMetric(float64(total), "total_messages")
		})
	}
}

// BenchmarkEventMarshal measures JSON serialization of events (part of bridge overhead).
func BenchmarkEventMarshal(b *testing.B) {
	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Result: &model.Document{
			ID:            "doc-1",
			EntityID:      "entity-1",
			SchemaName:    "pet",
			RecordVersion: 1,
			Data:          map[string]any{"name": "Fido", "status": "available"},
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(evt)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func formatN(n int) string {
	switch {
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
