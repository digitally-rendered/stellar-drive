package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	mongoadapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/mongo"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ReadinessCheck is a named health check function. It returns true when the
// subsystem is healthy and false otherwise.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) bool
}

// HealthHandler provides /health (liveness) and /ready (readiness) HTTP
// endpoints. Construct with NewHealthHandler and mount with Routes.
type HealthHandler struct {
	mongoConn    *mongoadapter.Connection
	registry     *schema.Registry
	checks       []ReadinessCheck
	readyTimeout time.Duration
}

// HealthOption configures a HealthHandler.
type HealthOption func(*HealthHandler)

// WithMongoConnection adds a MongoDB connectivity readiness check.
func WithMongoConnection(conn *mongoadapter.Connection) HealthOption {
	return func(h *HealthHandler) {
		h.mongoConn = conn
	}
}

// WithSchemaRegistry adds a schema-loaded readiness check.
func WithSchemaRegistry(reg *schema.Registry) HealthOption {
	return func(h *HealthHandler) {
		h.registry = reg
	}
}

// WithReadinessCheck adds a custom named readiness check.
func WithReadinessCheck(name string, fn func(ctx context.Context) bool) HealthOption {
	return func(h *HealthHandler) {
		h.checks = append(h.checks, ReadinessCheck{Name: name, Check: fn})
	}
}

// WithReadyTimeout sets the context timeout for readiness checks.
func WithReadyTimeout(d time.Duration) HealthOption {
	return func(h *HealthHandler) {
		h.readyTimeout = d
	}
}

// NewHealthHandler constructs a HealthHandler with the given options.
func NewHealthHandler(opts ...HealthOption) *HealthHandler {
	h := &HealthHandler{
		readyTimeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Routes returns a Chi router with /health and /ready endpoints.
func (h *HealthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)
	return r
}

// Health is a liveness probe that always returns 200 OK.
func (h *HealthHandler) Health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"status": "healthy"})
}

// Ready is a readiness probe that checks all configured subsystems.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readyTimeout)
	defer cancel()

	checks := make(map[string]bool)
	allReady := true

	// Built-in: MongoDB connectivity.
	if h.mongoConn != nil {
		ok := h.mongoConn.Ping(ctx) == nil
		checks["database"] = ok
		if !ok {
			allReady = false
		}
	}

	// Built-in: schemas loaded.
	if h.registry != nil {
		ok := len(h.registry.Names()) > 0
		checks["schemas"] = ok
		if !ok {
			allReady = false
		}
	}

	// Custom checks.
	for _, c := range h.checks {
		ok := c.Check(ctx)
		checks[c.Name] = ok
		if !ok {
			allReady = false
		}
	}

	status := http.StatusOK
	statusText := "ready"
	if !allReady {
		status = http.StatusServiceUnavailable
		statusText = "not_ready"
	}

	WriteJSON(w, status, map[string]any{
		"status": statusText,
		"checks": checks,
	})
}
