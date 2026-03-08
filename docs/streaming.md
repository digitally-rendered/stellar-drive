# Messaging and Streaming

Stellar-Drive can forward lifecycle events to external message brokers, enabling event-driven architectures, CQRS, real-time notifications, and cross-service integration.

## Architecture

The streaming system has three layers:

```
GenericCRUDService
       │
       ▼
  Event Bus (in-process, synchronous)
       │
       ▼
  EventBridge (subscriber → translator)
       │
       ▼
  EventStreamer port (broker-agnostic interface)
       │
       ├──▶ KafkaStreamer
       ├──▶ NATSStreamer
       └──▶ RedisStreamer
```

The **EventBridge** subscribes to all post-mutation events on the internal event bus, translates them into `StreamMessage` values, and publishes them to the configured broker adapter.

Key design decisions:
- Only **post-events** are streamed — confirmed outcomes, not intentions
- Only **mutation events** are streamed — creates, updates, deletes (not reads)
- Errors in streaming are **logged, never propagated** — a broker failure does not fail the API request

## Configuration

```yaml
# stellar.yaml
streaming:
  enabled: true
  adapter: "kafka"     # "kafka", "nats", or "redis"
```

Or via environment variables:

```bash
STELLAR_STREAMING_ENABLED=true
STELLAR_STREAMING_ADAPTER=kafka
```

### Broker-Specific Configuration

Each adapter accepts its own connection settings:

#### Kafka

```yaml
streaming:
  enabled: true
  adapter: "kafka"
  config:
    brokers:
      - "localhost:9092"
      - "kafka-2:9092"
```

Default brokers: `["localhost:9092"]`

#### NATS

```yaml
streaming:
  enabled: true
  adapter: "nats"
  config:
    url: "nats://localhost:4222"
```

Default URL: `nats://localhost:4222`

#### Redis Streams

```yaml
streaming:
  enabled: true
  adapter: "redis"
  config:
    addr: "localhost:6379"
```

Default address: `localhost:6379`

## Topic Naming Convention

Topics are derived from the schema name and event type:

| Event | Topic |
|-------|-------|
| `PostCreate` | `{schema}.created` |
| `PostUpdate` | `{schema}.updated` |
| `PostDelete` | `{schema}.deleted` |
| `PostBulkCreate` | `{schema}.bulk_created` |
| `PostBulkUpdate` | `{schema}.bulk_updated` |
| `PostBulkDelete` | `{schema}.bulk_deleted` |
| `PostGet` | `{schema}.retrieved` |
| `PostList` | `{schema}.listed` |

Examples:
```
pet.created
order.updated
user.deleted
pet.bulk_created
```

## Stream Message Format

Each message published to the broker has this structure:

```go
type StreamMessage struct {
    Topic   string            // e.g. "pet.created"
    Key     string            // entity ID (for partitioning)
    Payload []byte            // JSON-serialized event
    Headers map[string]string // metadata headers
}
```

### Payload

The payload is the full `event.Event` serialized as JSON:

```json
{
  "type": "post_create",
  "schema_name": "pet",
  "timestamp": "2024-01-15T10:30:00Z",
  "input": {
    "name": "Buddy",
    "status": "available"
  },
  "result": {
    "id": "doc-uuid",
    "entity_id": "entity-uuid",
    "record_version": 1,
    "data": {
      "name": "Buddy",
      "status": "available"
    },
    "created_at": "2024-01-15T10:30:00Z",
    "etag": "abc123..."
  },
  "metadata": {}
}
```

### Partition Key

The `Key` field is extracted from the event result:
- For single-document events (`*model.Document`): the `EntityID`
- For list results (`*model.ListResult`): empty string
- This enables ordered processing per entity in Kafka (same partition) and Redis Streams (same stream key)

## The EventStreamer Interface

To implement a custom broker adapter:

```go
type EventStreamer interface {
    // Publish sends a message to the broker
    Publish(ctx context.Context, msg *StreamMessage) error

    // Subscribe receives messages from a topic
    Subscribe(ctx context.Context, topic string,
        handler func(ctx context.Context, msg *StreamMessage) error) error

    // Close releases broker connections
    Close() error
}
```

The `EventBridge` only calls `Publish`. The `Subscribe` method is available for building consumers that react to externally published messages.

## Event Bridge Lifecycle

The bridge is created and started during engine setup:

```go
// Engine startup (simplified)
streamer := streaming.New(cfg.Streaming.Adapter, cfg.Streaming.Config)
bridge := streaming.NewEventBridge(eventBus, streamer)
bridge.Start() // subscribes to all 8 post-event types
```

`Start()` registers handlers for all post-event types in the global scope (schema name = ""), meaning it receives events for all schemas. The bridge:

1. Builds the topic from the schema name and event type
2. Extracts the entity ID for the partition key
3. JSON-serializes the event
4. Calls `streamer.Publish(ctx, msg)`
5. Logs and swallows any publish errors

## Data Flow Example

When a pet is created via REST:

```
POST /api/v1/pets {"name": "Buddy", "status": "available"}
  │
  ▼
GenericCRUDService.Create()
  │
  ├── eventBus.Publish(PreCreate)  ← pre-event handlers run
  │
  ├── MongoRepository.Create()     ← document persisted
  │
  └── eventBus.Publish(PostCreate) ← post-event handlers run
        │
        ├── AuditTrail handler     ← writes audit record
        │
        ├── EventBridge handler    ← translates to StreamMessage
        │     │
        │     └── KafkaStreamer.Publish(
        │           topic: "pet.created",
        │           key: "entity-uuid",
        │           payload: <full event JSON>
        │         )
        │
        └── GraphQL subscription   ← pushes to subscriber channels
```

## Consuming Events

### External Consumer Pattern

Build a service that subscribes to topics from the broker:

```go
// Example: external notification service
streamer := streaming.New("kafka", map[string]any{
    "brokers": []string{"localhost:9092"},
})

streamer.Subscribe(ctx, "pet.created", func(ctx context.Context, msg *StreamMessage) error {
    var evt event.Event
    json.Unmarshal(msg.Payload, &evt)

    doc := evt.Result.(*model.Document)
    fmt.Printf("New pet: %s (ID: %s)\n", doc.Data["name"], doc.EntityID)

    // Send notification, update search index, etc.
    return nil
})
```

### Consumer Group Patterns

For Kafka and Redis Streams, consumer groups enable horizontal scaling:

- **Kafka**: Use `group.id` in consumer configuration — each message is delivered to one consumer per group
- **Redis Streams**: Use `XREADGROUP` with consumer groups — each message is acknowledged individually
- **NATS**: Use JetStream consumers with durable names for at-least-once delivery

## Integration with the Event Bus

The streaming adapter complements the in-process event bus rather than replacing it:

| Feature | Event Bus | Streaming |
|---------|:---------:|:---------:|
| Transport | In-process | Network (broker) |
| Pre-events | Yes (can abort) | No |
| Post-events | Yes (fire-and-forget) | Yes |
| Cross-service | No | Yes |
| Persistence | No | Yes (broker-dependent) |
| Ordering | Synchronous | Per-partition |
| Delivery guarantee | At-most-once | Broker-dependent |

Use the event bus for:
- Validation guards (pre-events)
- In-process side effects (audit trail, cache invalidation)
- GraphQL subscriptions

Use streaming for:
- Cross-service communication
- Event sourcing / CQRS projections
- External notifications (email, SMS, push)
- Search index updates
- Analytics pipelines

## Related Guides

- [Events](events.md) — the internal event bus that powers streaming
- [Schema Lifecycle](schema-lifecycle.md) — how events fit into the request pipeline
- [GraphQL](graphql.md) — subscriptions (another event consumer)
- [Configuration](configuration.md) — full streaming configuration reference
