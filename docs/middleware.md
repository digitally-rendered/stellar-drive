# Middleware

Stellar-drive ships with eight built-in HTTP middleware functions that follow the standard Go/Chi pattern `func(http.Handler) http.Handler`. They live in `pkg/adapter/driving/rest/middleware/` and are composed into an ordered stack by the engine at startup.

## Architecture

Every middleware function accepts an `http.Handler` and returns a new `http.Handler` that wraps the original. This signature is compatible with Chi, the Go standard library, and any other router that follows the same convention:

```go
func MyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // pre-processing
        next.ServeHTTP(w, r)
        // post-processing
    })
}
```

Middleware that requires configuration parameters uses a constructor that returns the middleware function:

```go
func CORS(allowedOrigins []string) func(http.Handler) http.Handler
func Auth(secret string) func(http.Handler) http.Handler
func RateLimit(requestsPerSecond, burst int) func(http.Handler) http.Handler
```

## Chain

The `Chain` function composes multiple middleware into a single middleware function. Middleware is applied in left-to-right (outermost-first) order -- the first argument executes first on the way in and last on the way out.

```go
import "github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest/middleware"

stack := middleware.Chain(
    middleware.Recovery,
    middleware.RequestID,
    middleware.Logging,
    middleware.SecurityHeaders,
)

http.ListenAndServe(":8080", stack(router))
```

Internally, `Chain` applies the slice in reverse so the first middleware wraps outermost:

```go
func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(final http.Handler) http.Handler {
        h := final
        for i := len(middlewares) - 1; i >= 0; i-- {
            h = middlewares[i](h)
        }
        return h
    }
}
```

## Built-In Middleware

### 1. Recovery

Recovers from panics anywhere in the handler chain, logs the stack trace via `slog.Error`, and returns a `500 Internal Server Error` response. This prevents a single panicking request from crashing the entire process.

```go
middleware.Recovery(next http.Handler) http.Handler
```

Log output includes the recovered value, full stack trace, HTTP method, and request path.

### 2. RequestID

Ensures every request carries a unique identifier. If the incoming request has an `X-Request-ID` header, that value is used. Otherwise, a random 32-character hex string is generated.

```go
middleware.RequestID(next http.Handler) http.Handler
```

The ID is:
- Stored in the request context (retrievable via `middleware.RequestIDFromContext(ctx)`)
- Echoed back in the `X-Request-ID` response header
- Included in log entries when the Logging middleware is also active

```go
// Retrieve the request ID in a handler or downstream middleware.
id := middleware.RequestIDFromContext(r.Context())
```

### 3. Logging

Logs every HTTP request using Go's structured `slog` package. The log entry is emitted after the downstream handler completes, capturing the final status code and duration.

```go
middleware.Logging(next http.Handler) http.Handler
```

Log fields:

| Field | Description |
|-------|-------------|
| `method` | HTTP method (GET, POST, etc.) |
| `path` | Request URL path |
| `status` | HTTP response status code |
| `duration_ms` | Request duration in milliseconds |
| `request_id` | Request ID (when RequestID middleware is active) |

Example log output:

```json
{"level":"info","msg":"http request","method":"GET","path":"/api/v1/pet/","status":200,"duration_ms":3,"request_id":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"}
```

The Logging middleware wraps `http.ResponseWriter` to capture the status code written by the downstream handler, defaulting to 200 if `WriteHeader` is never called explicitly.

### 4. SecurityHeaders

Adds a standard set of security response headers to every response:

```go
middleware.SecurityHeaders(next http.Handler) http.Handler
```

| Header | Value | Purpose |
|--------|-------|---------|
| `X-Content-Type-Options` | `nosniff` | Prevent MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Disallow embedding in frames |
| `X-XSS-Protection` | `1; mode=block` | Enable XSS filter in older browsers |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains` | Enforce HTTPS for one year |

SecurityHeaders is enabled by default via the `middleware.security_headers: true` config option.

### 5. CORS

Handles Cross-Origin Resource Sharing based on a configured list of allowed origins.

```go
middleware.CORS(allowedOrigins []string) func(http.Handler) http.Handler
```

Behavior:
- If `allowedOrigins` contains `"*"`, all origins are allowed (development only).
- Otherwise, the `Origin` request header is matched against the allowed set; matching origins receive `Access-Control-Allow-Origin` with a `Vary: Origin` header.
- Preflight `OPTIONS` requests receive a `204 No Content` with CORS headers and do not reach downstream handlers.
- `Access-Control-Allow-Credentials` is always set to `true` when an origin matches.
- `Access-Control-Max-Age` is set to `86400` (one day) on preflight responses.
- Allowed methods: GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD.

### 6. Auth (JWT)

Enforces Bearer token authentication using HS256 JWT validation.

```go
middleware.Auth(secret string) func(http.Handler) http.Handler
```

Validation steps:
1. Extract the token from the `Authorization: Bearer <token>` header.
2. Decode the JWT header and confirm `alg=HS256`.
3. Compute HMAC-SHA256 over `header.payload` using the configured secret; compare with the provided signature using constant-time comparison.
4. Decode the payload claims.
5. Check the `exp` claim if present; reject expired tokens.

On success, the decoded claims `map[string]any` are stored in the request context. Retrieve them with:

```go
claims, ok := middleware.UserFromContext(r.Context())
if ok {
    userID := claims["sub"].(string)
}
```

On failure, the middleware responds with `401 Unauthorized` and a JSON error body:

```json
{"error":"auth: jwt: token has expired"}
```

### 7. RateLimit

Enforces per-client-IP token bucket rate limiting.

```go
middleware.RateLimit(requestsPerSecond, burst int) func(http.Handler) http.Handler
```

Parameters:
- `requestsPerSecond` -- steady-state refill rate (tokens per second).
- `burst` -- maximum tokens (initial and ceiling); allows short traffic spikes.

When a client exceeds its quota, the middleware responds with `429 Too Many Requests` and a `Retry-After` header indicating how many seconds to wait:

```json
{"error":"rate limit exceeded","retry_after":2}
```

Client IP extraction order:
1. `X-Forwarded-For` header (first entry if comma-separated)
2. `X-Real-IP` header
3. `RemoteAddr` (with port stripped)

Idle buckets are evicted automatically every 5 minutes to prevent unbounded memory growth.

### 8. ContentNegotiation

Handles content type negotiation for JSON, YAML, and XML based on the `Accept` and `Content-Type` headers. This middleware is opt-in and must be enabled in configuration.

```go
middleware.ContentNegotiation() func(http.Handler) http.Handler
```

**Request side**: If the `Content-Type` header is a YAML media type (`application/yaml`, `application/x-yaml`, or `text/yaml`), the request body is read, converted from YAML to JSON, and replaced. Downstream handlers always receive JSON.

**Response side**: If the `Accept` header is YAML or XML, the JSON response body is captured, deserialized, and re-serialized in the requested format:
- `application/yaml` / `application/x-yaml` / `text/yaml` -- YAML output
- `application/xml` / `text/xml` -- XML output with `<?xml version="1.0" encoding="UTF-8"?>` header

When `Accept` is `application/json` or absent, the response passes through unmodified.

## Default Middleware Stack

The engine assembles the middleware stack in `buildMiddleware()` using the following order:

```
Recovery -> RequestID -> Logging -> SecurityHeaders -> CORS -> [ContentNeg] -> custom
```

The exact stack depends on configuration:

| Middleware | When Applied |
|------------|-------------|
| Recovery | Always |
| RequestID | Always |
| Logging | Always |
| SecurityHeaders | When `middleware.security_headers: true` (default) |
| CORS | When `middleware.cors.allowed_origins` has entries |
| ContentNegotiation | When `middleware.content_negotiation: true` |
| Custom middleware | Appended last via `engine.WithMiddleware()` |

## Health and Ready Probes

The `/health` and `/ready` endpoints are mounted **outside** the middleware chain directly on the root router. This ensures:

- Health checks are not blocked by authentication middleware.
- Liveness probes always return quickly without rate limiting.
- Kubernetes and load balancers can reach probes even when the middleware stack is misconfigured.

## Custom Middleware

Add custom middleware to the engine via the `WithMiddleware` option:

```go
package main

import (
    "log/slog"
    "net/http"

    "github.com/digitally-rendered/stellar-drive/pkg/config"
    "github.com/digitally-rendered/stellar-drive/pkg/engine"
)

func tenantMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        tenant := r.Header.Get("X-Tenant-ID")
        if tenant == "" {
            http.Error(w, `{"error":"X-Tenant-ID header required"}`, http.StatusBadRequest)
            return
        }
        slog.Info("tenant request", "tenant", tenant, "path", r.URL.Path)
        next.ServeHTTP(w, r)
    })
}

func main() {
    cfg, _ := config.Load("stellar.yaml")

    e := engine.New(cfg,
        engine.WithMiddleware(tenantMiddleware),
    )
    e.Start(context.Background())
}
```

Custom middleware is inserted after all built-in middleware. If you need to run before the built-in stack, construct the Chi router manually using `middleware.Chain`.

## Configuration

The middleware section in `stellar.yaml` controls built-in middleware behavior:

```yaml
middleware:
  # CORS configuration.
  cors:
    allowed_origins:
      - "http://localhost:3000"
      - "https://app.example.com"

  # Enable security response headers (default: true).
  security_headers: true

  # Per-IP token bucket rate limiting.
  rate_limit:
    requests_per_second: 100
    burst: 50

  # Enable content negotiation for YAML/XML (default: false).
  content_negotiation: true

  # Enable structured request logging (default: true).
  request_logging: true

  # Trust proxy headers for IP extraction (default: false).
  trust_proxy: false
```

Authentication is configured separately under the `auth` section:

```yaml
auth:
  type: jwt
  jwt:
    secret: "your-secret-key-min-32-chars"
    issuer: "my-api"
```

## Writing Custom Middleware

When writing your own middleware, follow these conventions:

1. Accept `http.Handler`, return `http.Handler` -- this keeps your middleware compatible with Chi, the standard library, and stellar-drive's `Chain` function.
2. Use `context.WithValue` for request-scoped data. Define unexported context key types to avoid collisions.
3. Call `next.ServeHTTP(w, r)` to pass control to the next handler. Omit the call to short-circuit the chain (e.g., authentication failures).
4. Wrap `http.ResponseWriter` if you need to capture or modify the response.

```go
type contextKey string

const myDataKey contextKey = "my_data"

func MyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Add data to context.
        ctx := context.WithValue(r.Context(), myDataKey, "value")

        // Modify headers.
        w.Header().Set("X-Custom-Header", "hello")

        // Pass to next handler.
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

## Related Documentation

- **[Architecture](architecture.md)** -- Request/response flow showing where middleware executes in the pipeline
- **[Configuration](configuration.md)** -- Full `stellar.yaml` reference including middleware options
- **[Container](container.md)** -- DI container and how custom handlers bypass the middleware chain
