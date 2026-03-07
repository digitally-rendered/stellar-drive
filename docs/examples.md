# Examples

Practical code examples for common stellar-drive tasks, from a Petstore walkthrough to custom middleware, event handlers, and advanced features.

## Petstore Walkthrough

Stellar-drive bundles five Petstore schemas (`pet`, `order`, `user`, `category`, `tag`) in the `schemas/` directory.

### Start the Server

```yaml
# stellar.yaml
project:
  name: petstore
  version: "1.0.0"
server:
  port: 8080
  api_prefix: /api/v1
schemas:
  dir: ./schemas
mongo:
  uri: mongodb://localhost:27017
  database: petstore
middleware:
  cors:
    allowed_origins: ["*"]
  security_headers: true
```

```bash
stellar-drive run
```

### Create Entities

```bash
# Create a category
curl -X POST http://localhost:8080/api/v1/category/ \
  -H "Content-Type: application/json" \
  -d '{"name": "Dogs"}'

# Create a pet
curl -X POST http://localhost:8080/api/v1/pet/ \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Buddy",
    "photo_urls": ["https://example.com/buddy.jpg"],
    "status": "available",
    "category": {"name": "Dogs"},
    "tags": [{"name": "friendly"}]
  }'

# Create a user
curl -X POST http://localhost:8080/api/v1/user/ \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","first_name":"Alice","last_name":"Smith","email":"alice@example.com"}'

# Create an order
curl -X POST http://localhost:8080/api/v1/order/ \
  -H "Content-Type: application/json" \
  -d '{"pet_id":"a1b2c3d4","quantity":1,"status":"placed","complete":false}'
```

### Query with Filters

```bash
# Find available pets
curl -G http://localhost:8080/api/v1/pet/ \
  --data-urlencode 'where={"status":{"_eq":"available"}}'

# Pets whose name starts with "B"
curl -G http://localhost:8080/api/v1/pet/ \
  --data-urlencode 'where={"name":{"_starts_with":"B"}}'

# Combine with AND
curl -G http://localhost:8080/api/v1/pet/ \
  --data-urlencode 'where={"_and":[{"status":{"_eq":"available"}},{"name":{"_starts_with":"B"}}]}'

# Sort by name, limit to 5
curl "http://localhost:8080/api/v1/pet/?sort=name&limit=5"
```

---

## Custom Service Override

Use the DI container to replace the default service for a specific schema:

```go
// OrderService wraps the default service with custom order validation.
type OrderService struct {
    inner port.Service
}

func (s *OrderService) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
    if _, ok := data["pet_id"]; !ok {
        return nil, fmt.Errorf("pet_id is required for orders")
    }
    if _, ok := data["status"]; !ok {
        data["status"] = "placed"
    }
    return s.inner.Create(ctx, schemaName, data)
}

// Implement FindByID, List, Update, Delete by delegating to s.inner.
```

Register in the DI container:

```go
container.RegisterService("order", &OrderService{inner: defaultSvc})
```

---

## Event Handler Examples

### Validation Guard

Block documents that violate custom business rules using a pre-create event:

```go
func maxPriceGuard(maxPrice float64) event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        data, ok := evt.Input.(map[string]any)
        if !ok {
            return nil
        }
        price, _ := data["price"].(float64)
        if price > maxPrice {
            return fmt.Errorf("price %.2f exceeds maximum of %.2f", price, maxPrice)
        }
        return nil
    }
}

func setup(bus *event.Bus) {
    bus.Subscribe(event.PreCreate, "order", maxPriceGuard(10000.00))
    bus.Subscribe(event.PreUpdate, "order", maxPriceGuard(10000.00))
}
```

### Logging Hook

Log all mutations across every schema:

```go
func auditLogger(logger *slog.Logger) event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        logger.InfoContext(ctx, "audit",
            "event", string(evt.Type),
            "schema", evt.SchemaName,
            "timestamp", evt.Timestamp,
        )
        return nil
    }
}

func setup(bus *event.Bus) {
    handler := auditLogger(slog.Default())
    bus.Subscribe(event.PostCreate, "", handler)
    bus.Subscribe(event.PostUpdate, "", handler)
    bus.Subscribe(event.PostDelete, "", handler)
}
```

### Data Transformation

Normalize and enrich data before persistence:

```go
func normalizeEmail() event.Handler {
    return func(ctx context.Context, evt *event.Event) error {
        data, ok := evt.Input.(map[string]any)
        if !ok {
            return nil
        }
        if email, ok := data["email"].(string); ok {
            data["email"] = strings.ToLower(strings.TrimSpace(email))
        }
        return nil
    }
}

func setup(bus *event.Bus) {
    bus.Subscribe(event.PreCreate, "user", normalizeEmail())
    bus.Subscribe(event.PreUpdate, "user", normalizeEmail())
}
```

---

## Custom Middleware

Add application-specific middleware via the engine option:

```go
func apiKeyMiddleware(validKeys map[string]bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            key := r.Header.Get("X-API-Key")
            if !validKeys[key] {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusUnauthorized)
                w.Write([]byte(`{"error":"invalid or missing API key"}`))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

func main() {
    cfg, _ := config.Load("stellar.yaml")
    keys := map[string]bool{"key-abc123": true, "key-xyz789": true}

    e := engine.New(cfg, engine.WithMiddleware(apiKeyMiddleware(keys)))
    e.Start(context.Background())
}
```

Test:

```bash
curl http://localhost:8080/api/v1/pet/
# {"error":"invalid or missing API key"}

curl -H "X-API-Key: key-abc123" http://localhost:8080/api/v1/pet/
# Success
```

---

## JWT Authentication Setup

### Configuration

```yaml
auth:
  type: jwt
  jwt:
    secret: "my-secret-key-at-least-32-characters-long"
    issuer: "petstore-api"
```

### Make Authenticated Requests

```bash
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/pet/
```

### Access Claims in Handlers

```go
import "github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest/middleware"

func myHandler(w http.ResponseWriter, r *http.Request) {
    claims, ok := middleware.UserFromContext(r.Context())
    if !ok {
        http.Error(w, "unauthorized", 401)
        return
    }
    userID := claims["sub"].(string)
    role := claims["role"].(string)
    fmt.Fprintf(w, "Hello, %s (role: %s)", userID, role)
}
```

---

## Webhook Configuration

```yaml
webhooks:
  enabled: true
  max_retries: 3
  backoff_strategy: exponential
  initial_backoff: 1s
  max_backoff: 1m
  signing_enabled: true
  signing_secret: "webhook-secret-at-least-32-characters"
```

Verify signatures in your receiving service by computing HMAC-SHA256 over the raw body and comparing with the `X-Webhook-Signature` header:

```go
func verifyWebhookSignature(body []byte, signature, secret string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

---

## Field Projection

Reduce response payload size by requesting only specific fields:

```bash
curl "http://localhost:8080/api/v1/pet/a1b2c3d4?fields=name,status"
```

Response:

```json
{"success":true,"data":{"name":"Buddy","status":"available"}}
```

Works on list endpoints too:

```bash
curl "http://localhost:8080/api/v1/pet/?fields=name,status&limit=10"
```

---

## Content Negotiation

Enable in config:

```yaml
middleware:
  content_negotiation: true
```

### YAML Response

```bash
curl -H "Accept: application/yaml" http://localhost:8080/api/v1/pet/a1b2c3d4
```

### YAML Request Body

```bash
curl -X POST http://localhost:8080/api/v1/pet/ \
  -H "Content-Type: application/yaml" \
  -d '
name: Mittens
photo_urls:
  - https://example.com/mittens.jpg
status: available
'
```

### XML Response

```bash
curl -H "Accept: application/xml" http://localhost:8080/api/v1/pet/a1b2c3d4
```

---

## SQL Adapter Configuration

Configure PostgreSQL for specific schemas:

```yaml
sql:
  driver: postgres
  dsn: "postgres://stellar:password@localhost:5432/stellar_db?sslmode=disable"
  max_open_conns: 25
  auto_migrate: true
```

Point individual schemas at SQL storage in their envelope:

```json
{
  "name": "invoice",
  "version": "1.0.0",
  "storage": "sql",
  "table": "invoices",
  "schema": {
    "type": "object",
    "required": ["customer_id", "amount"],
    "properties": {
      "customer_id": {"type": "string"},
      "amount": {"type": "number"},
      "currency": {"type": "string", "default": "USD"}
    }
  }
}
```

Route to SQL via the DI container:

```go
c := container.New(
    container.WithDefaultRepository(mongoRepo),
    container.WithRepository("invoice", sqlRepo),
    container.WithDefaultService(defaultSvc),
)
```

---

## Audit Trail Setup

### Configuration

```yaml
audit:
  enabled: true
  collection: "_audit_log"
  track_reads: false
```

### Query the Audit Trail

```bash
# By entity
curl http://localhost:8080/api/v1/_audit/entity/a1b2c3d4

# By user
curl http://localhost:8080/api/v1/_audit/user/user-123

# Filtered
curl "http://localhost:8080/api/v1/_audit/entity/a1b2c3d4?schema=pet&limit=10"
```

### Custom Audit Handler

```go
func customAuditHandler(bus *event.Bus) {
    handler := func(ctx context.Context, evt *event.Event) error {
        slog.Info("external audit",
            "type", string(evt.Type),
            "schema", evt.SchemaName,
            "timestamp", evt.Timestamp,
        )
        return nil
    }
    bus.Subscribe(event.PostCreate, "", handler)
    bus.Subscribe(event.PostUpdate, "", handler)
    bus.Subscribe(event.PostDelete, "", handler)
}
```

---

## Runtime Schema Registration

Register a new schema without restarting the server:

```bash
curl -X POST http://localhost:8080/api/v1/_schemas \
  -H "Content-Type: application/json" \
  -d '{"name":"review","version":"1.0.0","storage":"mongo","collection":"reviews",
    "schema":{"type":"object","required":["product_id","rating"],"properties":{
      "product_id":{"type":"string"},"rating":{"type":"integer"},"comment":{"type":"string"}
    }}}'

# CRUD API is immediately available
curl -X POST http://localhost:8080/api/v1/review/ \
  -H "Content-Type: application/json" \
  -d '{"product_id":"prod-001","rating":5,"comment":"Excellent!"}'
```

---

## Health and Readiness Checks

### Custom Readiness Check

```go
healthHandler := rest.NewHealthHandler(
    rest.WithMongoConnection(mongoConn),
    rest.WithSchemaRegistry(registry),
    rest.WithReadinessCheck("cache", func(ctx context.Context) bool {
        return redisClient.Ping(ctx).Err() == nil
    }),
    rest.WithReadyTimeout(3 * time.Second),
)
```

### Test Probes

```bash
curl http://localhost:8080/health
# {"status":"healthy"}

curl http://localhost:8080/ready
# {"status":"ready","checks":{"database":true,"schemas":true,"cache":true}}
```

## Related Documentation

- **[Getting Started](getting-started.md)** -- First-run walkthrough
- **[API Reference](api-reference.md)** -- Complete endpoint reference
- **[Events](events.md)** -- Event handler API and all 16 lifecycle events
- **[Middleware](middleware.md)** -- Built-in and custom middleware
- **[Container](container.md)** -- DI container for service overrides
- **[Configuration](configuration.md)** -- Full `stellar.yaml` reference
- **[Code Generation](codegen.md)** -- Generating typed Go code from schemas
