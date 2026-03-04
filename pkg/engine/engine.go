// Package engine is the composition root for stellar-drive. It wires together
// all adapters, services, and infrastructure components into a running HTTP
// server. The Engine is the single entry point callers use to start and stop
// the server.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	mongoadapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/mongo"
	graphqladapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/graphql"
	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest"
	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest/middleware"
	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/core/service"
)

// Engine is the composition root that wires all components together. Construct
// it with New and start it with Start. All exported fields are safe to read
// after New returns; none should be mutated externally.
type Engine struct {
	cfg         *config.StellarConfig
	registry    *schema.Registry
	eventBus    *event.Bus
	ctr         *container.Container
	funcReg     *registry.FunctionRegistry
	mongoConn   *mongoadapter.Connection
	schemaStore *mongoadapter.MongoSchemaStore
	router      chi.Router
	server      *http.Server

	// Options applied at construction time.
	schemaDir      string
	extraMiddleware []func(http.Handler) http.Handler
}

// New constructs an Engine from cfg and applies any functional options. The
// resulting Engine must be started with Start; infrastructure connections are
// not established until then.
func New(cfg *config.StellarConfig, opts ...Option) *Engine {
	e := &Engine{
		cfg:      cfg,
		registry: schema.NewRegistry(),
		eventBus: event.NewBus(),
		funcReg:  registry.NewFunctionRegistry(),
	}

	// Derive schema directory from config; options may override.
	e.schemaDir = cfg.Schemas.Dir

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start wires all components, begins accepting HTTP connections, and blocks
// until a SIGINT or SIGTERM signal is received. It performs a graceful
// shutdown before returning.
func (e *Engine) Start(ctx context.Context) error {
	// 1. Connect to MongoDB.
	conn, err := mongoadapter.NewConnection(e.cfg.Mongo.URI, e.cfg.Mongo.Database)
	if err != nil {
		return fmt.Errorf("engine: create mongo connection: %w", err)
	}

	slog.InfoContext(ctx, "connecting to mongodb", "uri", e.cfg.Mongo.URI, "database", e.cfg.Mongo.Database)
	if err := conn.Connect(ctx); err != nil {
		return fmt.Errorf("engine: connect to mongodb: %w", err)
	}
	e.mongoConn = conn

	// 2. Create MongoRepository and MongoSchemaStore.
	repo := mongoadapter.NewRepository(conn)
	e.schemaStore = mongoadapter.NewSchemaStore(conn)

	// 3. Load schemas from the filesystem into the in-memory registry.
	if e.schemaDir != "" {
		slog.InfoContext(ctx, "loading schemas from filesystem", "dir", e.schemaDir)
		if err := e.loadSchemasFromDir(ctx, e.schemaDir, repo); err != nil {
			// Non-fatal: missing or empty schema directory is allowed on first run.
			slog.WarnContext(ctx, "schema directory load skipped", "dir", e.schemaDir, "error", err)
		}
	}

	// 4. Load schemas already persisted in the schema store into the registry.
	slog.InfoContext(ctx, "loading schemas from mongo schema store")
	if err := e.loadSchemasFromStore(ctx); err != nil {
		slog.WarnContext(ctx, "schema store load skipped", "error", err)
	}

	// 5. Create GenericCRUDService wired to the MongoDB repository.
	svc := service.NewGenericCRUDService(repo, e.eventBus, e.registry)

	// 6. Build the DI container with defaults.
	e.ctr = container.New(
		container.WithDefaultRepository(repo),
		container.WithDefaultService(svc),
		container.WithEventBus(e.eventBus),
	)

	// 7. Build the middleware stack.
	mwStack := e.buildMiddleware()

	// 8. Build the Chi router and mount all schema routes.
	apiPrefix := e.cfg.Server.APIPrefix
	dataRouter := rest.NewRouter(apiPrefix, e.registry, e.ctr, e.schemaStore)
	e.router = chi.NewRouter()
	e.router.Mount("/", middleware.Chain(mwStack...)(dataRouter))

	// 8a. Optionally mount the GraphQL endpoint.
	if e.cfg.GraphQL.Enabled {
		if err := e.mountGraphQL(ctx); err != nil {
			// Non-fatal: log and continue without GraphQL.
			slog.WarnContext(ctx, "graphql endpoint disabled", "error", err)
		}
	}

	// 9. Configure and start the HTTP server.
	addr := fmt.Sprintf("%s:%d", e.cfg.Server.Host, e.cfg.Server.Port)
	e.server = &http.Server{
		Addr:         addr,
		Handler:      e.router,
		ReadTimeout:  e.cfg.Server.ReadTimeout,
		WriteTimeout: e.cfg.Server.WriteTimeout,
	}

	// Listen first so that tests can connect immediately after Start returns.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("engine: listen on %s: %w", addr, err)
	}

	slog.InfoContext(ctx, "stellar-drive server starting",
		"address", addr,
		"api_prefix", apiPrefix,
		"project", e.cfg.Project.Name,
		"version", e.cfg.Project.Version,
	)

	serverErr := make(chan error, 1)
	go func() {
		if err := e.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// 10. Wait for shutdown signal or server error.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.InfoContext(ctx, "shutdown signal received", "signal", sig)
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("engine: server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.InfoContext(ctx, "context cancelled, shutting down")
	}

	// 11. Graceful shutdown with timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), e.cfg.Server.GracefulShutdown)
	defer cancel()

	return e.Shutdown(shutdownCtx)
}

// Shutdown gracefully stops the HTTP server and closes the MongoDB connection.
// It is safe to call from outside Start (e.g. tests).
func (e *Engine) Shutdown(ctx context.Context) error {
	var errs []error

	if e.server != nil {
		slog.InfoContext(ctx, "shutting down http server")
		if err := e.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http server shutdown: %w", err))
		}
	}

	if e.mongoConn != nil {
		slog.InfoContext(ctx, "closing mongodb connection")
		if err := e.mongoConn.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("mongodb close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Router returns the assembled Chi router. It is non-nil only after Start has
// been called (or after the internal wiring has completed). Intended for use
// in tests via httptest.NewServer.
func (e *Engine) Router() chi.Router {
	return e.router
}

// SchemaRegistry returns the schema registry populated during Start.
func (e *Engine) SchemaRegistry() *schema.Registry {
	return e.registry
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// loadSchemasFromDir reads *.schema.json files from dir, registers each
// envelope in the in-memory registry, and creates MongoDB indexes for every
// schema that declares them.
func (e *Engine) loadSchemasFromDir(ctx context.Context, dir string, repo *mongoadapter.MongoRepository) error {
	if err := schema.LoadFromDir(e.registry, dir); err != nil {
		return err
	}

	// Ensure indexes for each newly registered schema.
	for _, def := range e.registry.List() {
		if err := repo.CreateSchemaIndexes(ctx, def.Name, def.Indexes); err != nil {
			slog.WarnContext(ctx, "failed to create indexes for schema",
				"schema", def.Name, "error", err)
		}
	}
	return nil
}

// loadSchemasFromStore fetches all persisted schema envelopes from the Mongo
// schema store and registers any that are not already in the in-memory
// registry. Envelopes already loaded from the filesystem take precedence
// because filesystem schemas are canonical at startup.
func (e *Engine) loadSchemasFromStore(ctx context.Context) error {
	envelopes, err := e.schemaStore.List(ctx)
	if err != nil {
		return fmt.Errorf("list schemas from store: %w", err)
	}

	for _, env := range envelopes {
		if e.registry.Has(env.Name) {
			// Filesystem version already loaded; skip the persisted copy.
			continue
		}
		if _, err := e.registry.Register(env); err != nil {
			slog.WarnContext(ctx, "failed to register schema from store",
				"schema", env.Name, "version", env.Version, "error", err)
		}
	}
	return nil
}

// mountGraphQL builds the GraphQL schema from the registry and mounts the
// handler at the configured path (defaulting to "/graphql").
func (e *Engine) mountGraphQL(ctx context.Context) error {
	path := e.cfg.GraphQL.Path
	if path == "" {
		path = "/graphql"
	}

	gqlSchema, err := graphqladapter.BuildSchema(e.registry, e.ctr)
	if err != nil {
		return fmt.Errorf("build graphql schema: %w", err)
	}

	handler := graphqladapter.NewHandler(gqlSchema)
	e.router.Mount(path, handler)

	slog.InfoContext(ctx, "graphql endpoint mounted", "path", path)
	return nil
}

// buildMiddleware assembles the ordered middleware slice from config and any
// extra middleware provided via WithMiddleware.
func (e *Engine) buildMiddleware() []func(http.Handler) http.Handler {
	// Core middleware always applied: recovery, request ID, logging.
	mw := []func(http.Handler) http.Handler{
		middleware.Recovery,
		middleware.RequestID,
		middleware.Logging,
	}

	// Security headers are opt-in via config (default true).
	if e.cfg.Middleware.SecurityHeaders {
		mw = append(mw, middleware.SecurityHeaders)
	}

	// CORS is enabled when allowed origins are configured.
	if len(e.cfg.Middleware.CORS.AllowedOrigins) > 0 {
		mw = append(mw, middleware.CORS(e.cfg.Middleware.CORS.AllowedOrigins))
	}

	// Caller-supplied middleware is appended last (innermost after core).
	mw = append(mw, e.extraMiddleware...)

	return mw
}
