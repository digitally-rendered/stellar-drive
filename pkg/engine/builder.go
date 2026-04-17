package engine

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema/migration"
)

// Option is a functional option that configures an Engine before it starts.
type Option func(*Engine)

// WithSchemaDir overrides the schema directory path taken from
// cfg.Schemas.Dir. Useful when the directory is determined at runtime rather
// than via configuration file.
func WithSchemaDir(dir string) Option {
	return func(e *Engine) {
		e.schemaDir = dir
	}
}

// WithMiddleware appends one or more HTTP middleware functions to the engine's
// middleware stack. Custom middleware is inserted after the built-in core
// middleware (Recovery, RequestID, Logging, SecurityHeaders, CORS) so the
// built-in stack always executes first.
func WithMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(e *Engine) {
		e.extraMiddleware = append(e.extraMiddleware, mw...)
	}
}

// WithEventBus replaces the default in-process event bus with the supplied
// one. Use this to inject a pre-configured bus that already has subscribers
// registered, or to supply a test double.
func WithEventBus(bus *event.Bus) Option {
	return func(e *Engine) {
		e.eventBus = bus
	}
}

// WithFunctionRegistry replaces the default FunctionRegistry with the supplied
// one. Use this to pre-register custom handlers, guards, validators, or
// transforms before the engine starts.
func WithFunctionRegistry(reg *registry.FunctionRegistry) Option {
	return func(e *Engine) {
		e.funcReg = reg
	}
}

// WithPolicyEvaluator sets a pre-configured policy evaluator. When provided,
// the engine skips automatic evaluator construction from config. Use this to
// inject a custom evaluator or a test double.
func WithPolicyEvaluator(eval port.PolicyEvaluator) Option {
	return func(e *Engine) {
		e.policyEval = eval
	}
}

// WithMigrations installs a read-time schema-version migration registry. Every
// document returned from the repository is migrated from its persisted
// SchemaVersion up to the current version reported by the engine's schema
// registry. Writes are unaffected — documents are always written at the
// current schema version.
//
// Passing a nil registry is a no-op; this keeps WithMigrations safe to include
// unconditionally in wiring code.
func WithMigrations(reg *migration.Registry) Option {
	return func(e *Engine) {
		e.migrations = reg
	}
}

// WithRoutes registers caller-supplied HTTP routes that are mounted under the
// configured API prefix and share the built-in middleware stack. Routes are
// added before the schema-driven CRUD routes so custom paths cannot be shadowed
// by generic collection handlers.
//
// Use this for non-CRUD endpoints such as webhooks, search, reports, or
// aggregations. Multiple calls to WithRoutes are cumulative; each registration
// function runs against the same router.
func WithRoutes(register func(chi.Router)) Option {
	return func(e *Engine) {
		if register == nil {
			return
		}
		e.extraRoutes = append(e.extraRoutes, register)
	}
}
