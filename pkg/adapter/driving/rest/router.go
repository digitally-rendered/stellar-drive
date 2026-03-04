package rest

import (
	"github.com/go-chi/chi/v5"

	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/core/service"
)

// NewRouter creates the root Chi router, mounts the schema management API, and
// pre-mounts CRUD routes for all schemas that are already registered.
//
//   - apiPrefix   — path prefix, e.g. "/api/v1"
//   - registry    — schema registry (read at mount time; also passed to SchemaAPI)
//   - ctr         — DI container supplying services and repositories
//   - schemaStore — optional persistent store (may be nil)
func NewRouter(
	apiPrefix string,
	registry *schema.Registry,
	ctr *container.Container,
	schemaStore schema.Store,
) chi.Router {
	r := chi.NewRouter()

	// The schema management API needs a reference to the data router so it can
	// mount new schema routes dynamically at runtime.
	dataRouter := chi.NewRouter()

	schemaAPI := NewSchemaAPI(registry, schemaStore, ctr, dataRouter)

	// Mount the schema management sub-router.
	r.Mount(apiPrefix+"/_schemas", schemaAPI.Routes())

	// Pre-mount CRUD routes for schemas registered at startup.
	MountSchemaRoutes(dataRouter, "", registry, ctr)

	// Mount the data sub-router under the API prefix.
	r.Mount(apiPrefix, dataRouter)

	return r
}

// MountSchemaRoutes iterates over all schemas currently registered in registry
// and mounts a GenericHandler (or the container's custom handler) for each one
// onto router at "{apiPrefix}/{schemaName}".
//
// When apiPrefix is empty the route is mounted at "/{schemaName}" directly.
func MountSchemaRoutes(
	router chi.Router,
	apiPrefix string,
	registry *schema.Registry,
	ctr *container.Container,
) {
	for _, name := range registry.Names() {
		mountOne(router, apiPrefix, name, registry, ctr)
	}
}

// mountOne mounts a single schema's CRUD routes onto router.
func mountOne(
	router chi.Router,
	apiPrefix string,
	schemaName string,
	registry *schema.Registry,
	ctr *container.Container,
) {
	path := apiPrefix + "/" + schemaName

	if h, ok := ctr.ResolveHandler(schemaName); ok {
		router.Mount(path, h)
		return
	}

	svc := ctr.ResolveService(schemaName)
	if svc == nil {
		repo := ctr.DefaultRepository()
		if repo == nil {
			return
		}
		svc = service.NewGenericCRUDService(repo, ctr.EventBus(), registry)
	}

	handler := NewGenericHandler(schemaName, svc, registry)
	router.Mount(path, handler.Routes())
}
