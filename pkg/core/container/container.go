package container

import (
	"net/http"
	"sync"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// Container provides 4-layer dependency resolution per schema name.
//
// Resolution priority (highest to lowest):
//
//  1. Schema-specific override registered via RegisterX or WithX option.
//  2. Global default set via WithDefaultRepository / WithDefaultService.
//  3. nil (caller must handle the absent dependency).
//
// All methods are safe for concurrent use after construction.
type Container struct {
	mu             sync.RWMutex
	repositories   map[string]port.Repository
	services       map[string]port.Service
	handlers       map[string]http.Handler // schema -> custom HTTP handler

	defaultRepo    port.Repository
	defaultService port.Service

	eventBus port.EventBus
}

// New constructs an empty Container and applies all provided options. Options
// are applied in order, so later options can override earlier ones.
func New(opts ...Option) *Container {
	c := &Container{
		repositories: make(map[string]port.Repository),
		services:     make(map[string]port.Service),
		handlers:     make(map[string]http.Handler),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// ResolveRepository returns the repository for schemaName. Falls back to the
// default repository when no schema-specific override has been registered.
// Returns nil when neither is set.
func (c *Container) ResolveRepository(schemaName string) port.Repository {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if repo, ok := c.repositories[schemaName]; ok {
		return repo
	}
	return c.defaultRepo
}

// ResolveService returns the service for schemaName. Falls back to the default
// service when no schema-specific override has been registered. Returns nil
// when neither is set.
func (c *Container) ResolveService(schemaName string) port.Service {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if svc, ok := c.services[schemaName]; ok {
		return svc
	}
	return c.defaultService
}

// ResolveHandler returns the custom HTTP handler for schemaName, if one has
// been registered. The second return value is false when no handler is set for
// the schema.
func (c *Container) ResolveHandler(schemaName string) (http.Handler, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	h, ok := c.handlers[schemaName]
	return h, ok
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// RegisterRepository registers a schema-specific repository override. Calls
// made after construction are safe; subsequent ResolveRepository calls for the
// same schemaName will use the new value.
func (c *Container) RegisterRepository(schemaName string, repo port.Repository) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.repositories[schemaName] = repo
}

// RegisterService registers a schema-specific service override.
func (c *Container) RegisterService(schemaName string, svc port.Service) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.services[schemaName] = svc
}

// RegisterHandler registers a custom HTTP handler for schemaName. When set,
// the handler completely replaces the default CRUD HTTP handling for that
// schema.
func (c *Container) RegisterHandler(schemaName string, h http.Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers[schemaName] = h
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// EventBus returns the EventBus configured on this container, or nil if none
// was set.
func (c *Container) EventBus() port.EventBus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.eventBus
}

// DefaultRepository returns the global fallback repository.
func (c *Container) DefaultRepository() port.Repository {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.defaultRepo
}
