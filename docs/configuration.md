# Configuration Reference

This document describes all configuration options available in Stellar-drive. Configuration is defined in `stellar.yaml` and can be overridden via environment variables.

## Configuration File Location

Stellar-drive looks for configuration in this order:

1. File specified via `--config` flag: `stellar --config=/custom/path/stellar.yaml`
2. Current working directory: `./stellar.yaml`
3. Home directory: `~/.stellar/stellar.yaml`
4. System directory: `/etc/stellar/stellar.yaml`

## Full Annotated stellar.yaml

```yaml
# ============================================================================
# PROJECT IDENTITY
# ============================================================================
# Basic project metadata used in logging, metrics, and API responses.

project:
  # Service name. Appears in logs, metrics, and error responses.
  # Type: string, required
  # Default: (none)
  # Example: "petstore-api"
  name: my-api

  # Service version following semantic versioning (MAJOR.MINOR.PATCH).
  # Appears in API responses via GET /health or /api/v1/_system/version
  # Type: string, required
  # Default: (none)
  # Example: "1.0.0"
  version: "1.0.0"


# ============================================================================
# HTTP SERVER
# ============================================================================
# Configuration for the HTTP server and API behavior.

server:
  # TCP port the server listens on.
  # Type: integer, default: 8080
  # Example: 8080
  port: 8080

  # Hostname or IP address to bind to.
  # Type: string, default: "0.0.0.0"
  # Use "localhost" or "127.0.0.1" for local-only access.
  # Use "0.0.0.0" to listen on all interfaces.
  host: "0.0.0.0"

  # Timeout for reading the entire request (headers + body).
  # Prevents slow client attacks (Slowloris).
  # Type: duration (golang), default: 30s
  # Format: "30s", "1m", "500ms"
  # Example: "30s"
  read_timeout: 30s

  # Timeout for writing the entire response.
  # Prevents slow downstream clients from hanging the server.
  # Type: duration, default: 30s
  # Example: "30s"
  write_timeout: 30s

  # Timeout for graceful shutdown. Server waits this long for
  # in-flight requests to complete before force-killing.
  # Type: duration, default: 15s
  # Example: "15s"
  graceful_shutdown: 15s

  # API prefix for all dynamic routes (schemas, CRUD endpoints).
  # Type: string, default: "/api/v1"
  # All generated routes will be under this prefix.
  # Examples:
  #   - "/api/v1"     -> POST /api/v1/pet
  #   - "/api/v2"     -> POST /api/v2/pet
  #   - "/v1"         -> POST /v1/pet
  api_prefix: /api/v1


# ============================================================================
# SCHEMA LOADING
# ============================================================================
# Configuration for loading and managing JSON Schema envelopes.

schemas:
  # Directory containing schema envelope files.
  # Type: string, default: "./schemas"
  # Stellar-drive recursively scans this directory for .json and .yaml files.
  # Each file should be a valid SchemaEnvelope.
  dir: ./schemas

  # Enable hot reloading of schemas when files change.
  # Watches the schema directory and re-registers schemas on changes.
  # Type: bool, default: false
  # Warning: Experimental feature. May cause brief availability gaps.
  hot_reload: false


# ============================================================================
# MONGODB CONNECTION
# ============================================================================
# Configuration for MongoDB persistence adapter.
# Required if any schema specifies storage: "mongo"

mongo:
  # MongoDB connection URI.
  # Type: string, required (if using MongoDB)
  # Format: mongodb://[user:password@]host[:port][/database][?options]
  # Examples:
  #   - mongodb://localhost:27017
  #   - mongodb://user:pass@mongo.example.com:27017/stellar_db?ssl=true
  #   - mongodb+srv://user:pass@cluster.mongodb.net/stellar_db
  uri: mongodb://localhost:27017

  # Default database name.
  # Type: string, required (if using MongoDB)
  # Used if not specified in connection URI.
  # Collections are created here unless envelope specifies otherwise.
  database: my_api

  # Maximum size of connection pool.
  # Type: integer, default: 100
  # Tune based on expected concurrent requests.
  # Higher values consume more resources but handle more concurrency.
  max_pool_size: 100

  # Connection timeout for establishing new connections.
  # Type: duration, default: "10s"
  # Example: "10s"
  connect_timeout: 10s

  # Timeout for socket reads.
  # Type: duration, default: "30s"
  # Example: "30s"
  socket_timeout: 30s

  # Enable server selection timeout (time to find a suitable server).
  # Type: duration, default: "30s"
  server_selection_timeout: 30s

  # Authentication database (if using username/password).
  # Type: string, optional
  # Set to "admin" for MongoDB 4.0+ with standard auth setup.
  # auth_database: admin

  # Enable TLS/SSL connections.
  # Type: bool, default: false
  # If true, ensure URI includes ssl=true
  tls_enabled: false

  # Path to TLS certificate file.
  # Type: string, optional
  # Required if tls_enabled is true and using self-signed certs.
  # tls_cert_file: /etc/certs/mongo.crt


# ============================================================================
# SQL CONNECTION (Alternative to MongoDB)
# ============================================================================
# Configuration for SQL persistence adapter.
# Required if any schema specifies storage: "sql"

sql:
  # SQL driver to use.
  # Type: string, default: "postgres"
  # Supported: "postgres", "sqlite3"
  # Examples:
  #   - "postgres"  -> PostgreSQL
  #   - "sqlite3"   -> SQLite (file-based, single process)
  driver: postgres

  # Database connection string (DSN).
  # Type: string, required (if using SQL)
  # Format depends on driver:
  #
  # PostgreSQL:
  #   - "postgres://user:pass@localhost:5432/stellar_db?sslmode=disable"
  #   - "postgresql://user:pass@host/db?sslmode=require"
  #
  # SQLite3:
  #   - "file:./data.db?cache=shared"
  #   - "/var/lib/stellar/data.db"
  dsn: ""

  # Maximum number of open connections to the database.
  # Type: integer, default: 25
  # Tune based on database server limits and expected concurrency.
  max_open_conns: 25

  # Maximum number of connections allowed to sit idle in the pool.
  # Type: integer, default: 5
  # If max_idle_conns > max_open_conns, it's capped at max_open_conns.
  max_idle_conns: 5

  # Maximum lifetime of a reusable database connection.
  # Type: duration, default: "5m"
  # Connections older than this are closed and replaced.
  # Prevents stale connections with database-side timeouts.
  conn_max_lifetime: 5m

  # Connection timeout (time to acquire a connection from pool).
  # Type: duration, default: "30s"
  conn_timeout: 30s

  # Enable SSL/TLS for connections (PostgreSQL).
  # Type: bool, default: false
  # Set to true for prod databases. Use sslmode=require in DSN.
  ssl_enabled: false

  # Auto-migrate schemas when registering.
  # Type: bool, default: true
  # If true, Stellar-drive creates tables and indexes automatically.
  # Set to false if you manage schema via separate migrations.
  auto_migrate: true


# ============================================================================
# AUTHENTICATION
# ============================================================================
# Configuration for authentication and authorization.

auth:
  # Authentication type to enforce.
  # Type: string, default: "none"
  # Supported: "jwt", "none", "api_key"
  # Examples:
  #   - "jwt"      -> Validate JWT tokens in Authorization header
  #   - "none"     -> No authentication (public API)
  #   - "api_key"  -> Validate X-API-Key header
  type: jwt

  # JWT-specific configuration (required if type: "jwt").
  jwt:
    # Signing secret or public key.
    # Type: string, required (if type: "jwt")
    # For HS256 (HMAC): symmetric secret string
    # For RS256/ES256: PEM-encoded public key
    # Example: "your-secret-key-min-32-chars"
    secret: ""

    # JWT issuer claim to validate.
    # Type: string, optional
    # If set, validates that token iss claim matches.
    # Example: "https://auth.example.com"
    issuer: ""

    # JWT audience claim to validate.
    # Type: string, optional
    # If set, validates that token aud claim contains this value.
    # Example: "petstore-api"
    # audience: ""

    # JWT algorithm.
    # Type: string, default: "HS256"
    # Supported: "HS256", "HS384", "HS512", "RS256", "RS384", "RS512", "ES256", "ES384", "ES512"
    # algorithm: HS256

    # Enable JTI (JWT ID) claim validation for revocation.
    # Type: bool, default: false
    # If true, JTI must be present and checked against revocation list.
    # enable_jti: false

    # Claims to extract and pass to service (optional).
    # Type: array of strings
    # Example: ["sub", "email", "role"]
    # These claims are available in request context.
    # claims: []

  # API Key authentication (if type: "api_key").
  api_key:
    # Header name to check for API key.
    # Type: string, default: "X-API-Key"
    header: "X-API-Key"

    # Valid API keys.
    # Type: array of strings
    # Example: ["key-abc123", "key-xyz789"]
    # keys: []


# ============================================================================
# MIDDLEWARE
# ============================================================================
# Configuration for HTTP middleware and cross-cutting concerns.

middleware:
  # CORS (Cross-Origin Resource Sharing) configuration.
  cors:
    # Allowed origin patterns.
    # Type: array of strings, default: ["*"]
    # Use "*" to allow all origins (dev only).
    # For production, list specific domains: ["https://app.example.com"]
    allowed_origins:
      - "*"

    # Allowed HTTP methods.
    # Type: array of strings, default: ["GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"]
    # allowed_methods: []

    # Allowed request headers.
    # Type: array of strings, default: ["*"]
    # Use "*" to allow all headers (dev only).
    # For production: ["Content-Type", "Authorization"]
    # allowed_headers: []

    # Exposed response headers.
    # Type: array of strings, optional
    # Headers clients can access from responses.
    # Example: ["X-Total-Count", "X-Has-More"]
    # exposed_headers: []

    # Allow credentials (cookies, auth headers).
    # Type: bool, default: false
    # Set true if client sends credentials.
    # allow_credentials: false

    # Max age for preflight cache (seconds).
    # Type: integer, default: 86400 (1 day)
    # Clients cache preflight responses for this duration.
    # max_age: 86400

  # Security headers to inject in responses.
  # Type: bool, default: true
  # Adds: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, Strict-Transport-Security
  security_headers: true

  # Rate limiting configuration.
  rate_limit:
    # Requests allowed per second per client (IP or user).
    # Type: integer, default: 100
    # Set to 0 to disable rate limiting.
    requests_per_second: 100

    # Burst allowance (requests that can exceed rate in short time).
    # Type: integer, default: 50
    # Allows temporary spikes up to this many extra requests.
    burst: 50

    # Rate limit key strategy.
    # Type: string, default: "ip"
    # Supported: "ip" (by client IP), "user" (by authenticated user)
    # key_strategy: ip

  # Request logging.
  # Type: bool, default: true
  # Logs HTTP method, path, status, duration, size.
  request_logging: true

  # Trust proxy headers (X-Forwarded-For, X-Forwarded-Proto).
  # Type: bool, default: false
  # Set true if running behind reverse proxy (nginx, load balancer).
  # Affects IP extraction for rate limiting and logging.
  trust_proxy: false


# ============================================================================
# GRAPHQL
# ============================================================================
# Configuration for GraphQL API (optional feature).

graphql:
  # Enable GraphQL endpoint.
  # Type: bool, default: false
  # If false, GraphQL endpoint is not registered.
  enabled: false

  # URL path for GraphQL endpoint.
  # Type: string, default: "/graphql"
  # Full URL: http://localhost:8080/graphql
  path: /graphql

  # Enable GraphQL Playground (browser IDE).
  # Type: bool, default: false
  # If true, GraphQL Playground available at /graphql
  # Security: Disable in production!
  playground: false

  # Enable introspection queries.
  # Type: bool, default: true
  # If false, clients cannot introspect schema.
  # Security: Disable in production!
  introspection: true

  # Maximum query complexity (prevents abuse).
  # Type: integer, default: 1000
  # Query cost is calculated; exceeding this rejects request.
  max_complexity: 1000


# ============================================================================
# WEBHOOKS
# ============================================================================
# Configuration for webhook event dispatching.

webhooks:
  # Enable webhook event dispatching.
  # Type: bool, default: false
  enabled: false

  # Maximum number of retry attempts for failed deliveries.
  # Type: integer, default: 3
  # Fails after this many attempts; goes to dead-letter queue.
  max_retries: 3

  # Backoff strategy for retries.
  # Type: string, default: "exponential"
  # Supported: "exponential", "linear", "fixed"
  # exponential: 1s, 2s, 4s, 8s
  # linear: 1s, 2s, 3s, 4s
  # fixed: 1s, 1s, 1s, 1s
  backoff_strategy: exponential

  # Initial backoff duration.
  # Type: duration, default: "1s"
  initial_backoff: 1s

  # Maximum backoff duration.
  # Type: duration, default: "1m"
  max_backoff: 1m

  # Timeout for webhook delivery HTTP request.
  # Type: duration, default: "30s"
  timeout: 30s

  # Dead-letter queue collection/table.
  # Type: string, default: "_webhook_deadletter"
  # Failed webhooks stored here for manual retry.
  deadletter_store: "_webhook_deadletter"

  # Sign webhooks with HMAC.
  # Type: bool, default: true
  # Clients verify signature using X-Webhook-Signature header.
  signing_enabled: true

  # HMAC signing secret.
  # Type: string, required (if signing_enabled: true)
  # Shared secret between server and webhook consumer.
  # Example: "webhook-secret-min-32-chars"
  signing_secret: ""


# ============================================================================
# EVENT STREAMING
# ============================================================================
# Configuration for event streaming to external systems (optional).

streaming:
  # Enable event streaming.
  # Type: bool, default: false
  enabled: false

  # Streaming adapter to use.
  # Type: string, default: "kafka"
  # Supported: "kafka", "nats", "redis"
  # Examples:
  #   - "kafka"  -> Apache Kafka
  #   - "nats"   -> NATS Jetstream
  #   - "redis"  -> Redis Pub/Sub
  adapter: kafka

  # Kafka-specific configuration (if adapter: "kafka").
  kafka:
    # Broker addresses (comma-separated).
    # Type: string, required (if adapter: "kafka")
    # Example: "localhost:9092,localhost:9093"
    brokers: "localhost:9092"

    # Topic name for events.
    # Type: string, default: "stellar_events"
    topic: "stellar_events"

    # Consumer group ID.
    # Type: string, optional
    # For consuming events from the same topic.
    # consumer_group: "stellar_api"

    # SASL authentication (if enabled on broker).
    # Type: string, optional
    # Supported: "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"
    # sasl_mechanism: ""

    # SASL username.
    # Type: string, optional
    # sasl_user: ""

    # SASL password.
    # Type: string, optional
    # sasl_password: ""

  # NATS-specific configuration (if adapter: "nats").
  nats:
    # NATS server URLs.
    # Type: string, required (if adapter: "nats")
    # Example: "nats://localhost:4222"
    url: "nats://localhost:4222"

    # Jetstream subject for events.
    # Type: string, default: "stellar.events"
    subject: "stellar.events"

  # Redis-specific configuration (if adapter: "redis").
  redis:
    # Redis server address.
    # Type: string, required (if adapter: "redis")
    # Example: "localhost:6379"
    addr: "localhost:6379"

    # Redis channel name.
    # Type: string, default: "stellar_events"
    channel: "stellar_events"

    # Redis password (if auth enabled).
    # Type: string, optional
    # password: ""


# ============================================================================
# TELEMETRY / METRICS
# ============================================================================
# Configuration for observability (metrics, tracing).

telemetry:
  # Enable metrics collection and export.
  # Type: bool, default: false
  enabled: false

  # Metrics export format.
  # Type: string, default: "json"
  # Supported: "json", "prometheus", "statsd"
  # json:       JSON endpoint at /metrics
  # prometheus: Prometheus text format at /metrics
  # statsd:     Push to StatsD server
  exporter: json

  # HTTP endpoint path for metrics.
  # Type: string, default: "/metrics"
  # Available at http://localhost:8080/metrics (full path)
  endpoint: /metrics

  # Prometheus-specific configuration (if exporter: "prometheus").
  prometheus:
    # Prometheus HTTP endpoint (push mode).
    # Type: string, optional
    # If set, pushes metrics to Prometheus Pushgateway.
    # Example: "http://localhost:9091"
    # pushgateway: ""

  # StatsD-specific configuration (if exporter: "statsd").
  statsd:
    # StatsD server address.
    # Type: string, required (if exporter: "statsd")
    # Example: "localhost:8125"
    addr: "localhost:8125"

    # StatsD prefix for all metrics.
    # Type: string, optional
    # All metrics prefixed with this value.
    # Example: "stellar_api"
    # prefix: ""

  # Collect histogram metrics (response times, sizes).
  # Type: bool, default: true
  # Histograms are memory-intensive; disable if not needed.
  histograms: true

  # Collect per-endpoint metrics.
  # Type: bool, default: true
  # Detailed metrics per API endpoint.
  per_endpoint: true


# ============================================================================
# OPA POLICY ENGINE
# ============================================================================
# Configuration for Open Policy Agent (OPA) integration (optional).

policy:
  # Enable OPA policy engine.
  # Type: bool, default: false
  enabled: false

  # Directory containing OPA Rego policy files.
  # Type: string, default: "./policies"
  # Stellar-drive loads all .rego files from this directory.
  dir: ./policies

  # OPA server address (remote mode).
  # Type: string, optional
  # If set, uses remote OPA server instead of loading locally.
  # Example: "http://opa.example.com:8181"
  # server: ""

  # Default policy package to evaluate.
  # Type: string, default: "data.stellar"
  # Rego policies should define this package.
  # Example: "data.stellar.authz"
  package: "data.stellar"

  # Cache policy compilation.
  # Type: bool, default: true
  # Caches compiled policy for performance.
  cache_enabled: true

  # Cache TTL for policy rules.
  # Type: duration, default: "5m"
  cache_ttl: 5m


# ============================================================================
# LOGGING
# ============================================================================
# Configuration for application logging.

logging:
  # Log level.
  # Type: string, default: "info"
  # Supported: "debug", "info", "warn", "error"
  # debug: Verbose logging (development)
  # info:  Standard logging (production default)
  # warn:  Warnings and errors only
  # error: Errors only
  level: info

  # Log format.
  # Type: string, default: "json"
  # Supported: "json", "text"
  # json:  Structured JSON logs (production)
  # text:  Human-readable text (development)
  format: json

  # Log output destination.
  # Type: string, default: "stdout"
  # Supported: "stdout", "stderr", "file"
  # file: Requires output_file setting
  output: stdout

  # Log file path (if output: "file").
  # Type: string, optional
  # Example: "/var/log/stellar/server.log"
  # output_file: ""

  # Rotate log files.
  # Type: bool, default: false (if output: "file")
  # If true, creates daily rotated logs.
  rotate: false

  # Maximum size of log file before rotation (MB).
  # Type: integer, default: 100
  max_size_mb: 100

  # Maximum age of log file before deletion (days).
  # Type: integer, default: 30
  max_age_days: 30

  # Include caller info in logs (file and line number).
  # Type: bool, default: false
  # Useful for debugging; adds performance overhead.
  include_caller: false


# ============================================================================
# ENVIRONMENT VARIABLE OVERRIDES
# ============================================================================
# All configuration values can be overridden via environment variables.
# Prefix: STELLAR_
# Nested keys use underscores: server.port -> STELLAR_SERVER_PORT
#
# Examples:
#   STELLAR_SERVER_PORT=9090
#   STELLAR_SERVER_HOST=127.0.0.1
#   STELLAR_MONGO_URI=mongodb://prod:27017
#   STELLAR_MONGO_DATABASE=stellar_db
#   STELLAR_SQL_DRIVER=postgres
#   STELLAR_SQL_DSN=postgres://user:pass@host/db
#   STELLAR_AUTH_TYPE=jwt
#   STELLAR_AUTH_JWT_SECRET=supersecret
#   STELLAR_AUTH_JWT_ISSUER=https://auth.example.com
#   STELLAR_GRAPHQL_ENABLED=true
#   STELLAR_WEBHOOKS_ENABLED=true
#   STELLAR_WEBHOOKS_SIGNING_SECRET=webhook-secret
#   STELLAR_LOGGING_LEVEL=debug
#
# Environment variables take precedence over YAML configuration.
```

## Configuration Loading Order

1. Load defaults (embedded in binary)
2. Read YAML file (from config search paths)
3. Override with environment variables (STELLAR_* prefix)
4. Validate all required fields
5. Return error if invalid

## Environment Variable Override Examples

### Basic Server Config

```bash
export STELLAR_SERVER_PORT=9090
export STELLAR_SERVER_HOST=127.0.0.1
export STELLAR_SERVER_READ_TIMEOUT=60s
```

### Database Configuration

MongoDB:
```bash
export STELLAR_MONGO_URI=mongodb://user:pass@mongo.example.com:27017
export STELLAR_MONGO_DATABASE=stellar_db
export STELLAR_MONGO_MAX_POOL_SIZE=50
```

SQL (PostgreSQL):
```bash
export STELLAR_SQL_DRIVER=postgres
export STELLAR_SQL_DSN=postgres://user:pass@db.example.com:5432/stellar_db?sslmode=require
export STELLAR_SQL_MAX_OPEN_CONNS=25
```

### Authentication

```bash
export STELLAR_AUTH_TYPE=jwt
export STELLAR_AUTH_JWT_SECRET=$(openssl rand -base64 32)
export STELLAR_AUTH_JWT_ISSUER=https://auth.example.com
```

### Features

```bash
export STELLAR_GRAPHQL_ENABLED=true
export STELLAR_WEBHOOKS_ENABLED=true
export STELLAR_STREAMING_ENABLED=true
export STELLAR_TELEMETRY_ENABLED=true
export STELLAR_POLICY_ENABLED=true
```

### Logging

```bash
export STELLAR_LOGGING_LEVEL=debug
export STELLAR_LOGGING_FORMAT=text
export STELLAR_LOGGING_OUTPUT=stdout
```

## Configuration Examples

### Development Environment (stellar-dev.yaml)

```yaml
project:
  name: petstore-api
  version: "0.1.0"

server:
  port: 8080
  host: "localhost"
  api_prefix: /api/v1

schemas:
  dir: ./schemas
  hot_reload: true

mongo:
  uri: mongodb://localhost:27017
  database: petstore_dev

auth:
  type: none  # No auth for local development

middleware:
  cors:
    allowed_origins: ["*"]
  security_headers: false

graphql:
  enabled: true
  playground: true

logging:
  level: debug
  format: text
```

### Production Environment (stellar-prod.yaml)

```yaml
project:
  name: petstore-api
  version: "1.0.0"

server:
  port: 8080
  host: "0.0.0.0"
  graceful_shutdown: 30s
  api_prefix: /api/v1

schemas:
  dir: ./schemas
  hot_reload: false

sql:
  driver: postgres
  dsn: ""  # Set via STELLAR_SQL_DSN env var
  max_open_conns: 50
  auto_migrate: true

auth:
  type: jwt
  jwt:
    secret: ""  # Set via STELLAR_AUTH_JWT_SECRET env var
    issuer: "https://auth.example.com"

middleware:
  cors:
    allowed_origins:
      - "https://app.example.com"
      - "https://admin.example.com"
  security_headers: true
  rate_limit:
    requests_per_second: 1000
    burst: 100

webhooks:
  enabled: true
  max_retries: 5
  signing_secret: ""  # Set via STELLAR_WEBHOOKS_SIGNING_SECRET

streaming:
  enabled: true
  adapter: kafka
  kafka:
    brokers: "kafka-1:9092,kafka-2:9092,kafka-3:9092"

telemetry:
  enabled: true
  exporter: prometheus
  prometheus:
    pushgateway: "http://prometheus-pushgateway:9091"

policy:
  enabled: true
  dir: ./policies

logging:
  level: info
  format: json
  output: stdout
```

### Multi-Database Setup (stellar-multi.yaml)

```yaml
project:
  name: hybrid-api
  version: "2.0.0"

server:
  port: 8080
  api_prefix: /api/v2

# MongoDB for document-oriented schemas
mongo:
  uri: mongodb://mongo-cluster-0:27017,mongo-cluster-1:27017
  database: stellar_docs
  max_pool_size: 100

# PostgreSQL for relational schemas
sql:
  driver: postgres
  dsn: "postgres://user:pass@postgres:5432/stellar_relational"
  max_open_conns: 50

auth:
  type: jwt
  jwt:
    secret: ""  # Via env var
    issuer: "https://auth.company.com"

graphql:
  enabled: true
  path: /graphql
  introspection: false  # Disable in production

webhooks:
  enabled: true
  signing_enabled: true
  signing_secret: ""  # Via env var

streaming:
  enabled: true
  adapter: kafka
  kafka:
    brokers: "kafka:9092"
    sasl_mechanism: "SCRAM-SHA-256"
    sasl_user: ""  # Via env var
    sasl_password: ""  # Via env var

telemetry:
  enabled: true
  exporter: prometheus

logging:
  level: info
  format: json
```

## Validation Rules

The configuration loader validates:

1. **Required fields**: project.name, project.version
2. **Port range**: 1-65535
3. **Duration format**: Valid golang duration strings (e.g., "30s", "1m")
4. **Database URIs**: Valid connection strings for configured drivers
5. **Auth secrets**: Minimum length if type requires secrets
6. **Feature compatibility**: Cannot enable GraphQL if no driver configured, etc.

If validation fails, the server refuses to start and logs detailed errors.

## Configuration Best Practices

1. **Use environment variables for secrets**: Never commit secrets in stellar.yaml
2. **Version your configurations**: Use separate files for dev, staging, prod
3. **Test configuration changes**: Validate locally before deploying
4. **Document custom extensions**: If using x-stellar-* extensions, document them
5. **Monitor configuration drift**: Log config on startup
6. **Use sensible defaults**: Only override what differs from defaults
7. **Secure file permissions**: stellar.yaml may contain secrets; use chmod 600
8. **Rotate secrets regularly**: JWT secrets, webhook signing keys
9. **Test graceful shutdown**: Verify timeout is appropriate for your workload
10. **Monitor resource limits**: Watch connection pools, especially under load
