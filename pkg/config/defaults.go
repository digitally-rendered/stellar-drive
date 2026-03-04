package config

import "time"

// DefaultConfig returns a StellarConfig populated with sensible defaults.
// Values provided in a YAML file or environment variables will override these.
func DefaultConfig() *StellarConfig {
	return &StellarConfig{
		Project: ProjectConfig{
			Name:    "stellar-drive",
			Version: "0.1.0",
		},
		Server: ServerConfig{
			Port:             8080,
			Host:             "0.0.0.0",
			ReadTimeout:      30 * time.Second,
			WriteTimeout:     30 * time.Second,
			GracefulShutdown: 15 * time.Second,
			APIPrefix:        "/api/v1",
		},
		Schemas: SchemasConfig{
			Dir:       "./schemas",
			HotReload: false,
		},
		Mongo: MongoConfig{
			URI:         "mongodb://localhost:27017",
			Database:    "stellar_drive",
			MaxPoolSize: 100,
		},
		Middleware: MiddlewareConfig{
			SecurityHeaders: true,
			RateLimit: RateLimitConfig{
				RequestsPerSecond: 100,
				Burst:             50,
			},
		},
		GraphQL: GraphQLConfig{
			Path: "/graphql",
		},
	}
}
