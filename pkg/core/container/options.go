// Package container provides a 4-layer dependency-injection container that
// resolves repositories, services, and HTTP handlers per schema. The
// resolution order from highest to lowest priority is:
//
//  1. Schema-specific override (RegisterRepository, RegisterService, RegisterHandler)
//  2. Global default (WithDefaultRepository, WithDefaultService)
//  3. nil
//
// All options are applied during New() via the functional options pattern.
package container

import "github.com/digitally-rendered/stellar-drive/pkg/core/port"

// Option is a functional option that configures a Container.
type Option func(*Container)

// WithDefaultRepository sets the fallback repository used when no
// schema-specific repository has been registered.
func WithDefaultRepository(repo port.Repository) Option {
	return func(c *Container) {
		c.defaultRepo = repo
	}
}

// WithDefaultService sets the fallback service used when no schema-specific
// service has been registered.
func WithDefaultService(svc port.Service) Option {
	return func(c *Container) {
		c.defaultService = svc
	}
}

// WithEventBus sets the EventBus on the container. It is available to callers
// via Container.EventBus().
func WithEventBus(bus port.EventBus) Option {
	return func(c *Container) {
		c.eventBus = bus
	}
}

// WithRepository registers a schema-specific repository override. It takes
// precedence over the default repository for the named schema.
func WithRepository(schemaName string, repo port.Repository) Option {
	return func(c *Container) {
		c.repositories[schemaName] = repo
	}
}

// WithService registers a schema-specific service override. It takes
// precedence over the default service for the named schema.
func WithService(schemaName string, svc port.Service) Option {
	return func(c *Container) {
		c.services[schemaName] = svc
	}
}
