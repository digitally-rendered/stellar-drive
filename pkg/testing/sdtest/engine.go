package sdtest

import (
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driving/rest"
	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/core/service"
)

// TestEngine is a fully-wired, in-process stellar-drive stack backed by
// MemoryRepository. It is intended to be used with httptest.NewServer or
// httptest.NewRecorder in tests.
//
//	engine := sdtest.NewTestEngine(t, sdtest.WithSchemaJSON("pet", "1.0.0", petSchema))
//	srv    := httptest.NewServer(engine.Router)
//	defer srv.Close()
type TestEngine struct {
	// Router is the root Chi router. Pass it to httptest.NewServer.
	Router chi.Router

	// Registry holds every schema registered via TestOptions. Tests may call
	// Registry.Register to add more schemas after construction.
	Registry *schema.Registry

	// Container is the DI container wired with MemoryRepository and the
	// default service. Tests may call Container.RegisterService / RegisterHandler
	// to override behaviour for specific schemas.
	Container *container.Container

	// EventBus is the in-process event bus. Tests may subscribe to events
	// with EventBus.Subscribe before exercising the HTTP layer.
	EventBus *event.Bus

	// Repo is the backing in-memory repository. Inspect it directly to assert
	// state without going through the HTTP layer.
	Repo *MemoryRepository
}

// testConfig accumulates options before NewTestEngine builds the engine.
type testConfig struct {
	envelopes []*schema.SchemaEnvelope
}

// TestOption configures the TestEngine.
type TestOption func(*testConfig)

// WithSchema registers a pre-built SchemaEnvelope into the engine's registry.
func WithSchema(envelope *schema.SchemaEnvelope) TestOption {
	return func(cfg *testConfig) {
		cfg.envelopes = append(cfg.envelopes, envelope)
	}
}

// WithSchemaJSON is a convenience option that builds a SchemaEnvelope from the
// given name, version, and raw JSON Schema map and registers it.
func WithSchemaJSON(name, version string, schemaJSON map[string]any) TestOption {
	return WithSchema(&schema.SchemaEnvelope{
		Name:      name,
		Version:   version,
		Schema:    schemaJSON,
		Active:    true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

// NewTestEngine constructs a fully-wired stellar-drive stack backed by
// MemoryRepository and returns a TestEngine whose Router is ready to serve
// HTTP requests.
//
// The engine uses an empty API prefix so routes are mounted at "/{schemaName}".
// Schemas provided via TestOptions are registered before routes are mounted, so
// each schema gets its own CRUD endpoints immediately.
//
// t.Cleanup is used to reset the MemoryRepository after each test, ensuring
// test isolation when the engine is constructed at package level in a TestMain.
func NewTestEngine(t *testing.T, opts ...TestOption) *TestEngine {
	t.Helper()

	cfg := &testConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Layer 0: in-memory repository and event bus.
	repo := NewMemoryRepository()
	bus := event.NewBus()
	reg := schema.NewRegistry()

	// Register all schemas provided via options.
	for _, env := range cfg.envelopes {
		if _, err := reg.Register(env); err != nil {
			t.Fatalf("sdtest.NewTestEngine: register schema %q: %v", env.Name, err)
		}
	}

	// Default service wired against the in-memory repository.
	svc := service.NewGenericCRUDService(repo, bus, reg)

	// DI container with defaults.
	ctr := container.New(
		container.WithDefaultRepository(repo),
		container.WithDefaultService(svc),
		container.WithEventBus(bus),
	)

	// Build the router with an empty prefix and no persistent schema store or
	// function registry (both are optional in tests).
	funcReg := registry.NewFunctionRegistry()
	router := rest.NewRouter("", reg, ctr, nil, funcReg)

	// Reset state after the test completes so shared engines stay clean.
	t.Cleanup(func() {
		repo.Reset()
		bus.Clear()
	})

	return &TestEngine{
		Router:    router,
		Registry:  reg,
		Container: ctr,
		EventBus:  bus,
		Repo:      repo,
	}
}
