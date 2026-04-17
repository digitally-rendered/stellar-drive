// Package main is a runnable reference implementation showing how to embed
// stellar-drive as a library.
//
// It wires up the full OpenAPI Petstore (pet, order, user, category, tag) as
// versioned JSON-Schema-driven resources, with a handful of custom hooks that
// demonstrate every extension point: guards, validators, transforms, handler
// overrides, event subscribers, and custom middleware.
//
// Usage:
//
//	cd examples/petstore
//	go run .
//
// Requirements: a MongoDB instance reachable at the URI in stellar.yaml
// (defaults to mongodb://localhost:27017).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/engine"
)

func main() {
	cfg, err := config.Load("stellar.yaml")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	funcReg := registerPetHooks(registry.NewFunctionRegistry())
	bus := subscribeEvents(event.NewBus())

	eng := engine.New(
		cfg,
		engine.WithFunctionRegistry(funcReg),
		engine.WithEventBus(bus),
		engine.WithMiddleware(requestCountingMiddleware),
	)

	slog.Info("petstore example starting",
		"port", cfg.Server.Port,
		"schemas_dir", cfg.Schemas.Dir,
		"graphql", cfg.GraphQL.Enabled,
	)

	if err := eng.Start(context.Background()); err != nil {
		slog.Error("engine stopped with error", "error", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Registry hooks — guards, validators, transforms
// ---------------------------------------------------------------------------

func registerPetHooks(reg *registry.FunctionRegistry) *registry.FunctionRegistry {
	// Guard: only the admin role may soft-delete pets.
	reg.RegisterGuard("pet", registry.OpDelete, registry.Scope{}, adminOnlyGuard)

	// Validator: pet names must be at least 2 characters.
	reg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, minNameLengthValidator(2))
	reg.RegisterValidator("pet", registry.OpUpdate, registry.Scope{}, minNameLengthValidator(2))

	// Transform: canonicalise pet names on write (trim + lowercase).
	reg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, normaliseNameTransform)
	reg.RegisterTransform("pet", registry.OpUpdate, registry.Scope{}, normaliseNameTransform)

	return reg
}

func adminOnlyGuard(_ context.Context, r *http.Request) error {
	if r.Header.Get("X-Role") != "admin" {
		return errors.New("admin role required")
	}
	return nil
}

func minNameLengthValidator(minLen int) registry.ValidatorFunc {
	return func(_ context.Context, _ string, data map[string]any) error {
		name, _ := data["name"].(string)
		if len(strings.TrimSpace(name)) < minLen {
			return fmt.Errorf("name must be at least %d characters", minLen)
		}
		return nil
	}
}

func normaliseNameTransform(_ context.Context, _ string, data map[string]any) (map[string]any, error) {
	if s, ok := data["name"].(string); ok {
		data["name"] = strings.ToLower(strings.TrimSpace(s))
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Event subscribers
// ---------------------------------------------------------------------------

func subscribeEvents(bus *event.Bus) *event.Bus {
	// Subscribe to every pet lifecycle event. Pass "" as the schema name to
	// receive events for all schemas.
	bus.Subscribe(event.PostCreate, "pet", func(_ context.Context, e *event.Event) error {
		slog.Info("pet created", "schema", e.SchemaName)
		return nil
	})
	bus.Subscribe(event.PostDelete, "pet", func(_ context.Context, e *event.Event) error {
		slog.Info("pet deleted", "schema", e.SchemaName)
		return nil
	})
	return bus
}

// ---------------------------------------------------------------------------
// Custom middleware — append after the built-in stack.
// ---------------------------------------------------------------------------

func requestCountingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
