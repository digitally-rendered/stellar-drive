package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/core/service"
)

// SchemaAPI provides HTTP endpoints for runtime schema management.
// Version information lives inside the request/response body (the
// SchemaEnvelope), never in the URL path.
type SchemaAPI struct {
	registry  *schema.Registry
	store     schema.Store        // may be nil if no external store is configured
	container *container.Container
	funcReg   *registry.FunctionRegistry // may be nil
	router    chi.Router          // parent router; new schema routes are mounted here
}

// NewSchemaAPI constructs a SchemaAPI. store and funcReg may be nil; router is
// the parent chi.Router onto which dynamic schema CRUD routes will be mounted.
func NewSchemaAPI(
	reg *schema.Registry,
	store schema.Store,
	ctr *container.Container,
	router chi.Router,
	funcReg *registry.FunctionRegistry,
) *SchemaAPI {
	return &SchemaAPI{
		registry:  reg,
		store:     store,
		container: ctr,
		funcReg:   funcReg,
		router:    router,
	}
}

// Routes returns a chi.Router with all schema-management endpoints.
func (s *SchemaAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", s.RegisterSchema)
	r.Get("/", s.ListSchemas)
	r.Get("/{name}", s.GetSchema)
	r.Put("/{name}", s.UpdateSchema)
	r.Delete("/{name}", s.DeleteSchema)
	return r
}

// RegisterSchema handles POST /_schemas
//
// Flow:
//  1. Decode body as SchemaEnvelope
//  2. Validate the envelope
//  3. Register in schema.Registry
//  4. Persist to store (when configured)
//  5. Mount CRUD routes on the parent router
//  6. Return 201 with the registered envelope
func (s *SchemaAPI) RegisterSchema(w http.ResponseWriter, r *http.Request) {
	var envelope schema.SchemaEnvelope
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest("decode schema envelope: "+err.Error()))
		return
	}
	defer func() { _ = r.Body.Close() }()

	if err := schema.ValidateEnvelope(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	now := time.Now().UTC()
	if envelope.CreatedAt.IsZero() {
		envelope.CreatedAt = now
	}
	envelope.UpdatedAt = now
	envelope.Active = true

	if _, err := s.registry.Register(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest("register schema: "+err.Error()))
		return
	}

	if s.store != nil {
		if err := s.store.Save(r.Context(), &envelope); err != nil {
			// Log but do not abort — in-memory registry is the source of truth
			// for runtime routing; store persistence is best-effort here.
			_ = err
		}
	}

	s.mountSchemaRoutes(envelope.Name)

	WriteCreated(w, &envelope)
}

// ListSchemas handles GET /_schemas
// Returns the latest version of every registered schema.
func (s *SchemaAPI) ListSchemas(w http.ResponseWriter, r *http.Request) {
	defs := s.registry.List()
	WriteSuccess(w, defs)
}

// GetSchema handles GET /_schemas/{name}
// Resolves the version from the ?version= query param or the
// X-Schema-Version request header. Returns the latest version when neither
// is supplied.
func (s *SchemaAPI) GetSchema(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		WriteError(w, coreerrors.BadRequest("schema name is required"))
		return
	}

	version := r.URL.Query().Get("version")
	if version == "" {
		version = r.Header.Get("X-Schema-Version")
	}

	envelope, err := s.registry.GetEnvelope(name, version)
	if err != nil {
		WriteError(w, coreerrors.SchemaNotFound(name))
		return
	}

	WriteSuccess(w, envelope)
}

// UpdateSchema handles PUT /_schemas/{name}
// Re-registers the schema, which bumps the version pointer if the incoming
// version is newer. Persists to the store when configured.
func (s *SchemaAPI) UpdateSchema(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		WriteError(w, coreerrors.BadRequest("schema name is required"))
		return
	}

	var envelope schema.SchemaEnvelope
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest("decode schema envelope: "+err.Error()))
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Ensure the URL name and the envelope body are consistent.
	if envelope.Name == "" {
		envelope.Name = name
	}

	if err := schema.ValidateEnvelope(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	envelope.UpdatedAt = time.Now().UTC()
	envelope.Active = true

	if _, err := s.registry.Register(&envelope); err != nil {
		WriteError(w, coreerrors.BadRequest("update schema: "+err.Error()))
		return
	}

	if s.store != nil {
		_ = s.store.Save(r.Context(), &envelope)
	}

	// Ensure the parent router has a route for this schema name (idempotent).
	s.mountSchemaRoutes(envelope.Name)

	WriteSuccess(w, &envelope)
}

// DeleteSchema handles DELETE /_schemas/{name}
// Removes the schema from the in-memory registry and the persistent store.
func (s *SchemaAPI) DeleteSchema(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		WriteError(w, coreerrors.BadRequest("schema name is required"))
		return
	}

	if !s.registry.Has(name) {
		WriteError(w, coreerrors.SchemaNotFound(name))
		return
	}

	s.registry.Remove(name)

	if s.store != nil {
		_ = s.store.Delete(r.Context(), name)
	}

	WriteNoContent(w)
}

// mountSchemaRoutes creates a GenericHandler for schemaName and mounts its
// CRUD routes on the parent router at /api/v1/{schemaName}.
// When the container has a custom handler registered for the schema, that
// handler is used instead of the generic one.
func (s *SchemaAPI) mountSchemaRoutes(schemaName string) {
	if h, ok := s.container.ResolveHandler(schemaName); ok {
		s.router.Mount("/"+schemaName, h)
		return
	}

	svc := s.container.ResolveService(schemaName)
	if svc == nil {
		// Fall back to a GenericCRUDService backed by the default repository.
		repo := s.container.DefaultRepository()
		if repo == nil {
			// No repository available — skip mounting.
			return
		}
		svc = service.NewGenericCRUDService(repo, s.container.EventBus(), s.registry)
	}

	var opts []HandlerOption
	if s.funcReg != nil {
		opts = append(opts, WithFunctionRegistry(s.funcReg))
	}

	handler := NewGenericHandler(schemaName, svc, s.registry, opts...)
	s.router.Mount("/"+schemaName, handler.Routes())
}
