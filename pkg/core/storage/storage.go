// Package storage provides per-schema backend routing configuration.
// It lets the engine direct individual schemas to different storage backends
// (MongoDB, SQL) while keeping a sensible project-wide default.
package storage

import (
	"fmt"
	"sort"
)

// Backend identifies a storage backend by name.
type Backend string

const (
	// Mongo routes the schema's data to the MongoDB adapter.
	Mongo Backend = "mongo"
	// SQL routes the schema's data to the SQL adapter.
	SQL Backend = "sql"
)

// knownBackends is the canonical set of valid Backend values. Extend this when
// new adapters are added.
var knownBackends = map[Backend]struct{}{
	Mongo: {},
	SQL:   {},
}

// ConfigOption is a functional option for NewConfig.
type ConfigOption func(*Config)

// WithDefault sets the backend returned by Get for schemas that have no
// explicit mapping. If not supplied, the default is Mongo.
func WithDefault(b Backend) ConfigOption {
	return func(c *Config) {
		c.defaultBackend = b
	}
}

// Config holds the project-wide default backend and any per-schema overrides.
// The zero value is not usable; create instances with NewConfig.
type Config struct {
	defaultBackend Backend
	schemas        map[string]Backend // schema name -> backend
}

// NewConfig constructs a Config with Mongo as the default and applies any
// provided options.
func NewConfig(opts ...ConfigOption) *Config {
	c := &Config{
		defaultBackend: Mongo,
		schemas:        make(map[string]Backend),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the backend for schemaName. If no explicit mapping exists the
// project-wide default is returned.
func (c *Config) Get(schemaName string) Backend {
	if b, ok := c.schemas[schemaName]; ok {
		return b
	}
	return c.defaultBackend
}

// Set records an explicit backend mapping for schemaName. It returns an error
// when backend is not one of the known Backend constants so that callers catch
// typos at configuration time rather than at query time.
func (c *Config) Set(schemaName string, backend Backend) error {
	if _, ok := knownBackends[backend]; !ok {
		return fmt.Errorf("storage: unknown backend %q; valid values are %q and %q", backend, Mongo, SQL)
	}
	c.schemas[schemaName] = backend
	return nil
}

// SQLSchemas returns the names of schemas explicitly mapped to the SQL backend
// in lexicographic order.
func (c *Config) SQLSchemas() []string {
	return c.schemasFor(SQL)
}

// MongoSchemas returns the names of schemas explicitly mapped to the MongoDB
// backend in lexicographic order.
func (c *Config) MongoSchemas() []string {
	return c.schemasFor(Mongo)
}

// schemasFor collects schema names mapped to the given backend.
func (c *Config) schemasFor(b Backend) []string {
	var names []string
	for name, backend := range c.schemas {
		if backend == b {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
