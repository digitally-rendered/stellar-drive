package config

import "time"

// StellarConfig is the top-level configuration for a stellar-drive server instance.
type StellarConfig struct {
	Project    ProjectConfig    `yaml:"project"    json:"project"`
	Server     ServerConfig     `yaml:"server"     json:"server"`
	Schemas    SchemasConfig    `yaml:"schemas"    json:"schemas"`
	Mongo      MongoConfig      `yaml:"mongo"      json:"mongo"`
	SQL        SQLConfig        `yaml:"sql"        json:"sql"`
	Auth       AuthConfig       `yaml:"auth"       json:"auth"`
	Middleware MiddlewareConfig `yaml:"middleware"  json:"middleware"`
	GraphQL    GraphQLConfig    `yaml:"graphql"    json:"graphql"`
	Health     HealthConfig     `yaml:"health"     json:"health"`
	Audit      AuditConfig      `yaml:"audit"      json:"audit"`
	Webhooks   WebhooksConfig   `yaml:"webhooks"   json:"webhooks"`
	Streaming  StreamingConfig  `yaml:"streaming"  json:"streaming"`
	Telemetry  TelemetryConfig  `yaml:"telemetry"  json:"telemetry"`
	Policy     PolicyConfig     `yaml:"policy"     json:"policy"`
	Storage    StorageConfig    `yaml:"storage"    json:"storage"`
}

// ProjectConfig identifies the running service.
type ProjectConfig struct {
	Name    string `yaml:"name"    json:"name"`
	Version string `yaml:"version" json:"version"`
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	Port             int           `yaml:"port"              json:"port"`
	Host             string        `yaml:"host"              json:"host"`
	ReadTimeout      time.Duration `yaml:"read_timeout"      json:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout"     json:"write_timeout"`
	GracefulShutdown time.Duration `yaml:"graceful_shutdown" json:"graceful_shutdown"`
	APIPrefix        string        `yaml:"api_prefix"        json:"api_prefix"`
}

// SchemasConfig controls JSON Schema loading.
type SchemasConfig struct {
	Dir       string `yaml:"dir"        json:"dir"`
	HotReload bool   `yaml:"hot_reload" json:"hot_reload"`
}

// MongoConfig contains connection settings for MongoDB.
type MongoConfig struct {
	URI         string `yaml:"uri"           json:"uri"`
	Database    string `yaml:"database"      json:"database"`
	MaxPoolSize int    `yaml:"max_pool_size" json:"max_pool_size"`
}

// SQLConfig contains connection settings for a SQL database.
type SQLConfig struct {
	Driver       string `yaml:"driver"         json:"driver"`
	DSN          string `yaml:"dsn"            json:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns" json:"max_open_conns"`
}

// AuthConfig selects and configures the authentication strategy.
type AuthConfig struct {
	Type string    `yaml:"type" json:"type"`
	JWT  JWTConfig `yaml:"jwt"  json:"jwt"`
}

// JWTConfig holds JWT signing settings.
type JWTConfig struct {
	Secret string `yaml:"secret" json:"secret"`
	Issuer string `yaml:"issuer" json:"issuer"`
}

// MiddlewareConfig groups optional HTTP middleware settings.
type MiddlewareConfig struct {
	RateLimit           RateLimitConfig       `yaml:"rate_limit"            json:"rate_limit"`
	CORS                CORSConfig            `yaml:"cors"                  json:"cors"`
	SecurityHeaders     bool                  `yaml:"security_headers"      json:"security_headers"`
	SecurityHeadersOpts SecurityHeadersConfig `yaml:"security_headers_opts" json:"security_headers_opts"`
	ContentNegotiation  bool                  `yaml:"content_negotiation"   json:"content_negotiation"`
	ETag                bool                  `yaml:"etag"                  json:"etag"`
	Cache               CacheConfig           `yaml:"cache"                 json:"cache"`
}

// SecurityHeadersConfig provides fine-grained control over the security
// headers middleware when security_headers is enabled.
type SecurityHeadersConfig struct {
	HSTS              bool              `yaml:"hsts"               json:"hsts"`
	ReferrerPolicy    string            `yaml:"referrer_policy"    json:"referrer_policy"`
	PermissionsPolicy string            `yaml:"permissions_policy" json:"permissions_policy"`
	CustomHeaders     map[string]string `yaml:"custom_headers"     json:"custom_headers"`
}

// CacheConfig controls the response caching middleware.
type CacheConfig struct {
	Enabled    bool          `yaml:"enabled"     json:"enabled"`
	TTL        time.Duration `yaml:"ttl"         json:"ttl"`
	MaxEntries int           `yaml:"max_entries" json:"max_entries"`
}

// StorageConfig controls per-schema storage backend routing.
type StorageConfig struct {
	Default string            `yaml:"default" json:"default"` // "mongo" or "sql"
	Schemas map[string]string `yaml:"schemas" json:"schemas"` // schema name -> backend
}

// RateLimitConfig controls the token-bucket rate limiter.
type RateLimitConfig struct {
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int `yaml:"burst"               json:"burst"`
}

// CORSConfig lists origins that are permitted cross-origin access.
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins" json:"allowed_origins"`
}

// GraphQLConfig enables and configures the GraphQL endpoint.
type GraphQLConfig struct {
	Enabled    bool   `yaml:"enabled"    json:"enabled"`
	Path       string `yaml:"path"       json:"path"`
	Playground bool   `yaml:"playground" json:"playground"`
}

// WebhooksConfig controls outbound webhook delivery.
type WebhooksConfig struct {
	Enabled    bool `yaml:"enabled"     json:"enabled"`
	MaxRetries int  `yaml:"max_retries" json:"max_retries"`
}

// StreamingConfig selects the event-streaming adapter.
type StreamingConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Adapter string `yaml:"adapter" json:"adapter"`
}

// TelemetryConfig controls OpenTelemetry export.
type TelemetryConfig struct {
	Enabled  bool   `yaml:"enabled"  json:"enabled"`
	Exporter string `yaml:"exporter" json:"exporter"`
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

// HealthConfig controls liveness and readiness probe endpoints.
type HealthConfig struct {
	Enabled      bool          `yaml:"enabled"       json:"enabled"`
	ReadyTimeout time.Duration `yaml:"ready_timeout" json:"ready_timeout"`
}

// AuditConfig controls the audit trail subsystem.
type AuditConfig struct {
	Enabled    bool   `yaml:"enabled"     json:"enabled"`
	Collection string `yaml:"collection"  json:"collection"`
	TrackReads bool   `yaml:"track_reads" json:"track_reads"`
}

// PolicyConfig controls OPA / policy engine integration.
type PolicyConfig struct {
	Enabled       bool          `yaml:"enabled"        json:"enabled"`
	Mode          string        `yaml:"mode"           json:"mode"`           // "inline" or "remote"
	Dir           string        `yaml:"dir"            json:"dir"`            // policy file directory
	RemoteURL     string        `yaml:"remote_url"     json:"remote_url"`     // OPA server URL
	DefaultPolicy string        `yaml:"default_policy" json:"default_policy"` // e.g. "authz/allow"
	Timeout       time.Duration `yaml:"timeout"        json:"timeout"`        // HTTP timeout for remote OPA
	SkipPaths     []string      `yaml:"skip_paths"     json:"skip_paths"`     // URL prefixes to skip
}
