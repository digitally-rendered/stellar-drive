package registry

import "sync"

// FunctionRegistry stores custom overrides keyed by schema name and operation.
// All four override kinds — handlers, guards, validators, and transforms — are
// stored with their Scope so that ResolveX methods can apply specificity
// resolution at call time.
//
// All methods are safe for concurrent use.
type FunctionRegistry struct {
	mu         sync.RWMutex
	handlers   map[string]map[Operation][]scopedHandler
	guards     map[string]map[Operation][]scopedGuard
	validators map[string]map[Operation][]scopedValidator
	transforms map[string]map[Operation][]scopedTransform
}

// NewFunctionRegistry returns an empty, initialised FunctionRegistry.
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{
		handlers:   make(map[string]map[Operation][]scopedHandler),
		guards:     make(map[string]map[Operation][]scopedGuard),
		validators: make(map[string]map[Operation][]scopedValidator),
		transforms: make(map[string]map[Operation][]scopedTransform),
	}
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// RegisterHandler registers a HandlerFunc override for the given schema and
// operation. When a handler resolves for a request, the default service
// pipeline is bypassed entirely.
func (r *FunctionRegistry) RegisterHandler(schemaName string, op Operation, scope Scope, handler HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.handlers[schemaName] == nil {
		r.handlers[schemaName] = make(map[Operation][]scopedHandler)
	}
	r.handlers[schemaName][op] = append(r.handlers[schemaName][op], scopedHandler{scope: scope, handler: handler})
}

// RegisterGuard registers a GuardFunc for the given schema and operation.
// Multiple guards may be registered; they are all evaluated in specificity
// order and the first error short-circuits evaluation.
func (r *FunctionRegistry) RegisterGuard(schemaName string, op Operation, scope Scope, guard GuardFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.guards[schemaName] == nil {
		r.guards[schemaName] = make(map[Operation][]scopedGuard)
	}
	r.guards[schemaName][op] = append(r.guards[schemaName][op], scopedGuard{scope: scope, guard: guard})
}

// RegisterValidator registers a ValidatorFunc for the given schema and
// operation. Multiple validators may be registered and are all executed.
func (r *FunctionRegistry) RegisterValidator(schemaName string, op Operation, scope Scope, validator ValidatorFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.validators[schemaName] == nil {
		r.validators[schemaName] = make(map[Operation][]scopedValidator)
	}
	r.validators[schemaName][op] = append(r.validators[schemaName][op], scopedValidator{scope: scope, validator: validator})
}

// RegisterTransform registers a TransformFunc for the given schema and
// operation. Multiple transforms may be registered and are applied in
// specificity order.
func (r *FunctionRegistry) RegisterTransform(schemaName string, op Operation, scope Scope, transform TransformFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.transforms[schemaName] == nil {
		r.transforms[schemaName] = make(map[Operation][]scopedTransform)
	}
	r.transforms[schemaName][op] = append(r.transforms[schemaName][op], scopedTransform{scope: scope, transform: transform})
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// ResolveHandler returns the most specific matching HandlerFunc for the given
// schema, operation, version, and channel. If no handler is registered, the
// second return value is false.
func (r *FunctionRegistry) ResolveHandler(schemaName string, op Operation, version, channel string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := r.handlers[schemaName][op]
	if len(entries) == 0 {
		return nil, false
	}

	sorted := sortHandlersBySpecificity(entries, version, channel)
	if len(sorted) == 0 {
		return nil, false
	}
	return sorted[0].handler, true
}

// ResolveGuards returns all guards that match the given schema, operation,
// version, and channel, ordered from most to least specific.
func (r *FunctionRegistry) ResolveGuards(schemaName string, op Operation, version, channel string) []GuardFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return filterGuards(r.guards[schemaName][op], version, channel)
}

// ResolveValidators returns all validators that match the given schema,
// operation, version, and channel, ordered from most to least specific.
func (r *FunctionRegistry) ResolveValidators(schemaName string, op Operation, version, channel string) []ValidatorFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return filterValidators(r.validators[schemaName][op], version, channel)
}

// ResolveTransforms returns all transforms that match the given schema,
// operation, version, and channel, ordered from most to least specific.
func (r *FunctionRegistry) ResolveTransforms(schemaName string, op Operation, version, channel string) []TransformFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return filterTransforms(r.transforms[schemaName][op], version, channel)
}
