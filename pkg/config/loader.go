package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Load reads the YAML file at path, merges it on top of DefaultConfig, then
// applies any matching STELLAR_* environment variable overrides.
//
// If path is empty the function returns DefaultConfig with env overrides applied.
func Load(path string) (*StellarConfig, error) {
	cfg := DefaultConfig()

	if path != "" {
		if err := loadYAML(path, cfg); err != nil {
			return nil, fmt.Errorf("config load %q: %w", path, err)
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

// loadYAML decodes the YAML file at path into dst.
func loadYAML(path string, dst *StellarConfig) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode yaml: %w", err)
	}
	return nil
}

// applyEnv overrides cfg fields with values from well-known environment
// variables. Only variables that are actually set in the environment take
// effect; unset variables leave the existing value unchanged.
func applyEnv(cfg *StellarConfig) {
	// --- Server ---
	if v := os.Getenv("STELLAR_SERVER_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}
	if v := os.Getenv("STELLAR_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("STELLAR_SERVER_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ReadTimeout = d
		}
	}
	if v := os.Getenv("STELLAR_SERVER_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.WriteTimeout = d
		}
	}
	if v := os.Getenv("STELLAR_SERVER_GRACEFUL_SHUTDOWN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.GracefulShutdown = d
		}
	}
	if v := os.Getenv("STELLAR_SERVER_API_PREFIX"); v != "" {
		cfg.Server.APIPrefix = v
	}

	// --- MongoDB ---
	if v := os.Getenv("STELLAR_MONGO_URI"); v != "" {
		cfg.Mongo.URI = v
	}
	if v := os.Getenv("STELLAR_MONGO_DATABASE"); v != "" {
		cfg.Mongo.Database = v
	}
	if v := os.Getenv("STELLAR_MONGO_MAX_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Mongo.MaxPoolSize = n
		}
	}

	// --- SQL ---
	if v := os.Getenv("STELLAR_SQL_DRIVER"); v != "" {
		cfg.SQL.Driver = v
	}
	if v := os.Getenv("STELLAR_SQL_DSN"); v != "" {
		cfg.SQL.DSN = v
	}
	if v := os.Getenv("STELLAR_SQL_MAX_OPEN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SQL.MaxOpenConns = n
		}
	}

	// --- Auth ---
	if v := os.Getenv("STELLAR_AUTH_TYPE"); v != "" {
		cfg.Auth.Type = v
	}
	if v := os.Getenv("STELLAR_AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWT.Secret = v
	}
	if v := os.Getenv("STELLAR_AUTH_JWT_ISSUER"); v != "" {
		cfg.Auth.JWT.Issuer = v
	}

	// --- Schemas ---
	if v := os.Getenv("STELLAR_SCHEMAS_DIR"); v != "" {
		cfg.Schemas.Dir = v
	}
	if v := os.Getenv("STELLAR_SCHEMAS_HOT_RELOAD"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Schemas.HotReload = b
		}
	}

	// --- Middleware / Rate limit ---
	if v := os.Getenv("STELLAR_MIDDLEWARE_RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Middleware.RateLimit.RequestsPerSecond = n
		}
	}
	if v := os.Getenv("STELLAR_MIDDLEWARE_RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Middleware.RateLimit.Burst = n
		}
	}
	if v := os.Getenv("STELLAR_MIDDLEWARE_SECURITY_HEADERS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Middleware.SecurityHeaders = b
		}
	}

	// --- GraphQL ---
	if v := os.Getenv("STELLAR_GRAPHQL_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.GraphQL.Enabled = b
		}
	}
	if v := os.Getenv("STELLAR_GRAPHQL_PATH"); v != "" {
		cfg.GraphQL.Path = v
	}
	if v := os.Getenv("STELLAR_GRAPHQL_PLAYGROUND"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.GraphQL.Playground = b
		}
	}

	// --- Webhooks ---
	if v := os.Getenv("STELLAR_WEBHOOKS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Webhooks.Enabled = b
		}
	}
	if v := os.Getenv("STELLAR_WEBHOOKS_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Webhooks.MaxRetries = n
		}
	}

	// --- Streaming ---
	if v := os.Getenv("STELLAR_STREAMING_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Streaming.Enabled = b
		}
	}
	if v := os.Getenv("STELLAR_STREAMING_ADAPTER"); v != "" {
		cfg.Streaming.Adapter = v
	}

	// --- Telemetry ---
	if v := os.Getenv("STELLAR_TELEMETRY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Telemetry.Enabled = b
		}
	}
	if v := os.Getenv("STELLAR_TELEMETRY_EXPORTER"); v != "" {
		cfg.Telemetry.Exporter = v
	}
	if v := os.Getenv("STELLAR_TELEMETRY_ENDPOINT"); v != "" {
		cfg.Telemetry.Endpoint = v
	}

	// --- Policy ---
	if v := os.Getenv("STELLAR_POLICY_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Policy.Enabled = b
		}
	}
	if v := os.Getenv("STELLAR_POLICY_DIR"); v != "" {
		cfg.Policy.Dir = v
	}
}
