package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

func TestHealthHandler_Health(t *testing.T) {
	h := NewHealthHandler()
	r := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "healthy", body["status"])
}

func TestHealthHandler_Ready(t *testing.T) {
	tests := []struct {
		name       string
		opts       []HealthOption
		wantStatus int
		wantBody   string
	}{
		{
			name:       "no checks configured returns ready",
			opts:       nil,
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			name: "schemas loaded returns ready",
			opts: []HealthOption{
				WithSchemaRegistry(registryWithSchemas(t)),
			},
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			name: "empty registry returns not_ready",
			opts: []HealthOption{
				WithSchemaRegistry(schema.NewRegistry()),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not_ready",
		},
		{
			name: "custom check passes",
			opts: []HealthOption{
				WithReadinessCheck("custom", func(_ context.Context) bool { return true }),
			},
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			name: "custom check fails",
			opts: []HealthOption{
				WithReadinessCheck("custom", func(_ context.Context) bool { return false }),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not_ready",
		},
		{
			name: "one passing and one failing check returns not_ready",
			opts: []HealthOption{
				WithReadinessCheck("ok", func(_ context.Context) bool { return true }),
				WithReadinessCheck("fail", func(_ context.Context) bool { return false }),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not_ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHealthHandler(tt.opts...)
			r := h.Routes()

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			var body map[string]any
			require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
			assert.Equal(t, tt.wantBody, body["status"])
		})
	}
}

// registryWithSchemas returns a schema.Registry with at least one schema registered.
func registryWithSchemas(t *testing.T) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	env := &schema.SchemaEnvelope{
		Name:    "test",
		Version: "1.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	}
	_, err := reg.Register(env)
	require.NoError(t, err)
	return reg
}
