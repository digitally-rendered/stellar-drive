package engine

import (
	"net/http"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
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
