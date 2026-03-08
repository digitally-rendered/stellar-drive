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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"

	mongoadapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/mongo"
	policyadapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/policy"
	graphqladapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/graphql"
	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest"
	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest/middleware"
	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
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
	policyEval  port.PolicyEvaluator
	mongoConn   *mongoadapter.Connection
	schemaStore *mongoadapter.MongoSchemaStore
	router      chi.Router
	server      *http.Server

	// Schema hot-reload watcher (nil when hot_reload is disabled).
	schemaWatcher *schema.SchemaWatcher

	// Options applied at construction time.
	schemaDir       string
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

	// 4a. Optionally start the schema hot-reload watcher.
	if e.cfg.Schemas.HotReload && e.schemaDir != "" {
		watcher := schema.NewSchemaWatcher(e.schemaDir, e.registry,
			schema.WithOnReload(func(name, version string) {
				slog.InfoContext(ctx, "schema hot-reloaded", "name", name, "version", version)
			}),
		)
		watcher.Start(ctx)
		e.schemaWatcher = watcher
		slog.InfoContext(ctx, "schema hot-reload watcher started", "dir", e.schemaDir)
	}

	// 5. Create GenericCRUDService wired to the MongoDB repository.
	svc := service.NewGenericCRUDService(repo, e.eventBus, e.registry)

	// 5a. Optionally wire the audit trail.
	var auditStore *mongoadapter.MongoAuditStore
	if e.cfg.Audit.Enabled {
		auditStoreOpts := []mongoadapter.AuditStoreOption{}
		if e.cfg.Audit.Collection != "" {
			auditStoreOpts = append(auditStoreOpts, mongoadapter.WithAuditCollection(e.cfg.Audit.Collection))
		}
		auditStore = mongoadapter.NewAuditStore(conn, auditStoreOpts...)
		audit := service.NewAuditTrail(auditStore, service.WithTrackReads(e.cfg.Audit.TrackReads))
		audit.Register(e.eventBus)
		slog.InfoContext(ctx, "audit trail enabled", "collection", e.cfg.Audit.Collection)
	}

	// 6. Build the DI container with defaults.
	e.ctr = container.New(
		container.WithDefaultRepository(repo),
		container.WithDefaultService(svc),
		container.WithEventBus(e.eventBus),
	)

	// 6a. Optionally build the policy evaluator from config.
	if e.cfg.Policy.Enabled && e.policyEval == nil {
		eval, err := e.buildPolicyEvaluator(ctx)
		if err != nil {
			slog.WarnContext(ctx, "policy evaluator disabled", "error", err)
		} else {
			e.policyEval = eval
			slog.InfoContext(ctx, "policy evaluator enabled", "mode", e.cfg.Policy.Mode)
		}
	}

	// 7. Build the middleware stack.
	mwStack := e.buildMiddleware()

	// 8. Build the Chi router and mount all schema routes.
	apiPrefix := e.cfg.Server.APIPrefix
	dataRouter := rest.NewRouter(apiPrefix, e.registry, e.ctr, e.schemaStore, e.funcReg)
	e.router = chi.NewRouter()
	e.router.Mount("/", middleware.Chain(mwStack...)(dataRouter))

	// 8a. Register health and readiness probes outside the middleware chain.
	if e.cfg.Health.Enabled {
		healthOpts := []rest.HealthOption{
			rest.WithMongoConnection(conn),
			rest.WithSchemaRegistry(e.registry),
		}
		if e.cfg.Health.ReadyTimeout > 0 {
			healthOpts = append(healthOpts, rest.WithReadyTimeout(e.cfg.Health.ReadyTimeout))
		}
		healthHandler := rest.NewHealthHandler(healthOpts...)
		e.router.Get("/health", healthHandler.Health)
		e.router.Get("/ready", healthHandler.Ready)
	}

	// 8b. Optionally mount the audit API.
	if e.cfg.Audit.Enabled && auditStore != nil {
		auditAPI := rest.NewAuditAPI(auditStore)
		e.router.Mount(apiPrefix+"/_audit", auditAPI.Routes())
	}

	// 8c. Optionally mount the GraphQL endpoint.
	if e.cfg.GraphQL.Enabled {
		if err := e.mountGraphQL(ctx); err != nil {
			// Non-fatal: log and continue without GraphQL.
			slog.WarnContext(ctx, "graphql endpoint disabled", "error", err)
		}
	}

	// 8d. Mount the live OpenAPI specification endpoint.
	e.router.Get("/openapi.json", rest.NewOpenAPIHandler(
		e.registry,
		e.cfg.Project.Name,
		e.cfg.Project.Version,
		e.cfg.GraphQL.Versioned,
	))
	slog.InfoContext(ctx, "openapi endpoint mounted",
		"path", "/openapi.json",
		"versioned", e.cfg.GraphQL.Versioned,
	)

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

	if e.schemaWatcher != nil {
		slog.InfoContext(ctx, "stopping schema watcher")
		e.schemaWatcher.Stop()
	}

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
// When cfg.GraphQL.Versioned is true, BuildVersionedSchema is used so that
// every registered schema version gets its own set of typed fields; otherwise
// only the latest version of each schema is exposed via BuildSchema.
func (e *Engine) mountGraphQL(ctx context.Context) error {
	path := e.cfg.GraphQL.Path
	if path == "" {
		path = "/graphql"
	}

	build := graphqladapter.BuildSchema
	if e.cfg.GraphQL.Versioned {
		build = graphqladapter.BuildVersionedSchema
	}
	gqlSchema, err := build(e.registry, e.ctr)
	if err != nil {
		return fmt.Errorf("build graphql schema: %w", err)
	}

	handler := graphqladapter.NewHandler(gqlSchema)
	e.router.Mount(path, handler)

	slog.InfoContext(ctx, "graphql endpoint mounted",
		"path", path,
		"versioned", e.cfg.GraphQL.Versioned,
	)
	return nil
}

// buildPolicyEvaluator constructs the appropriate policy evaluator based on
// the configured mode ("inline" or "remote").
func (e *Engine) buildPolicyEvaluator(ctx context.Context) (port.PolicyEvaluator, error) {
	switch e.cfg.Policy.Mode {
	case "remote":
		opts := []policyadapter.RemoteOPAOption{}
		if e.cfg.Policy.RemoteURL != "" {
			opts = append(opts, policyadapter.WithOPAURL(e.cfg.Policy.RemoteURL))
		}
		if e.cfg.Policy.DefaultPolicy != "" {
			opts = append(opts, policyadapter.WithDefaultPolicy(e.cfg.Policy.DefaultPolicy))
		}
		if e.cfg.Policy.Timeout > 0 {
			opts = append(opts, policyadapter.WithOPATimeout(e.cfg.Policy.Timeout))
		}
		return policyadapter.NewRemoteOPAEvaluator(opts...), nil

	default: // "inline"
		eval := policyadapter.NewInlineEvaluator()
		if e.cfg.Policy.Dir != "" {
			if err := e.loadPolicyFiles(ctx, eval); err != nil {
				slog.WarnContext(ctx, "policy files load skipped", "dir", e.cfg.Policy.Dir, "error", err)
			}
		}
		return eval, nil
	}
}

// loadPolicyFiles reads *.rego files from the configured policy directory
// and loads them into the evaluator via LoadPolicy.
func (e *Engine) loadPolicyFiles(ctx context.Context, eval port.PolicyEvaluator) error {
	pattern := filepath.Join(e.cfg.Policy.Dir, "*.rego")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob policy files: %w", err)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			slog.WarnContext(ctx, "failed to read policy file", "file", f, "error", err)
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f), ".rego")
		if err := eval.LoadPolicy(ctx, name, data); err != nil {
			slog.WarnContext(ctx, "failed to load policy", "file", f, "error", err)
		} else {
			slog.InfoContext(ctx, "loaded policy file", "name", name, "file", f)
		}
	}
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
		secOpts := []middleware.SecurityHeadersOption{}
		shCfg := e.cfg.Middleware.SecurityHeadersOpts
		if shCfg.HSTS {
			secOpts = append(secOpts, middleware.WithHSTS(true))
		}
		if shCfg.ReferrerPolicy != "" {
			secOpts = append(secOpts, middleware.WithReferrerPolicy(shCfg.ReferrerPolicy))
		}
		if shCfg.PermissionsPolicy != "" {
			secOpts = append(secOpts, middleware.WithPermissionsPolicy(shCfg.PermissionsPolicy))
		}
		if len(shCfg.CustomHeaders) > 0 {
			secOpts = append(secOpts, middleware.WithCustomHeaders(shCfg.CustomHeaders))
		}
		mw = append(mw, middleware.NewSecurityHeaders(secOpts...))
	}

	// ETag / conditional request handling (opt-in).
	if e.cfg.Middleware.ETag {
		mw = append(mw, middleware.ETag())
	}

	// CORS is enabled when allowed origins are configured.
	if len(e.cfg.Middleware.CORS.AllowedOrigins) > 0 {
		mw = append(mw, middleware.CORS(e.cfg.Middleware.CORS.AllowedOrigins))
	}

	// Content negotiation (opt-in).
	if e.cfg.Middleware.ContentNegotiation {
		mw = append(mw, middleware.ContentNegotiation())
	}

	// Response caching (opt-in).
	if e.cfg.Middleware.Cache.Enabled {
		cacheOpts := []middleware.CacheOption{}
		if e.cfg.Middleware.Cache.TTL > 0 {
			cacheOpts = append(cacheOpts, middleware.WithCacheTTL(e.cfg.Middleware.Cache.TTL))
		}
		if e.cfg.Middleware.Cache.MaxEntries > 0 {
			cacheOpts = append(cacheOpts, middleware.WithMaxCacheEntries(e.cfg.Middleware.Cache.MaxEntries))
		}
		mw = append(mw, middleware.Cache(cacheOpts...))
		slog.Info("response cache middleware enabled")
	}

	// Policy enforcement (opt-in).
	if e.cfg.Policy.Enabled && e.policyEval != nil {
		policyOpts := []middleware.PolicyOption{}
		if len(e.cfg.Policy.SkipPaths) > 0 {
			policyOpts = append(policyOpts, middleware.WithSkipPaths(e.cfg.Policy.SkipPaths...))
		}
		mw = append(mw, middleware.Policy(e.policyEval, policyOpts...))
	}

	// Caller-supplied middleware is appended last (innermost after core).
	mw = append(mw, e.extraMiddleware...)

	return mw
}
