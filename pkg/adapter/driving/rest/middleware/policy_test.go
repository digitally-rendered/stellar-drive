package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// mockEvaluator implements port.PolicyEvaluator for testing.
type mockEvaluator struct {
	allowed    bool
	reason     string
	err        error
	lastInput  *port.PolicyInput
}

func (m *mockEvaluator) Evaluate(_ context.Context, input *port.PolicyInput) (bool, string, error) {
	m.lastInput = input
	return m.allowed, m.reason, m.err
}

func (m *mockEvaluator) LoadPolicy(_ context.Context, _ string, _ []byte) error {
	return nil
}

func newPolicyTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestPolicy_Allowed(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `"ok":true`)
}

func TestPolicy_Denied(t *testing.T) {
	eval := &mockEvaluator{allowed: false, reason: "admin role required"}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pet/123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	errBody, _ := body["error"].(map[string]any)
	assert.Equal(t, "FORBIDDEN", errBody["code"])
	assert.Equal(t, "admin role required", errBody["message"])
}

func TestPolicy_EvaluatorError(t *testing.T) {
	eval := &mockEvaluator{err: fmt.Errorf("connection refused")}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	errBody, _ := body["error"].(map[string]any)
	assert.Equal(t, "INTERNAL", errBody["code"])
}

func TestPolicy_SkipPaths(t *testing.T) {
	eval := &mockEvaluator{allowed: false, reason: "should not reach"}
	handler := Policy(eval)(newPolicyTestHandler())

	skipPaths := []string{"/health", "/ready", "/_topology", "/docs", "/openapi.json"}
	for _, path := range skipPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code, "path %s should be skipped", path)
		})
	}
}

func TestPolicy_CustomSkipPaths(t *testing.T) {
	eval := &mockEvaluator{allowed: false}
	handler := Policy(eval, WithSkipPaths("/custom", "/metrics"))(newPolicyTestHandler())

	// Custom skip path should pass through.
	req := httptest.NewRequest(http.MethodGet, "/custom/endpoint", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Default skip path should NOT be skipped (overridden).
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestPolicy_DefaultInputBuilder(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "create", eval.lastInput.Action)
	assert.Equal(t, "pet", eval.lastInput.SchemaName)
	assert.Equal(t, "POST", eval.lastInput.Context["method"])
	assert.Equal(t, "/api/v1/pet", eval.lastInput.Context["path"])

	pathParts, ok := eval.lastInput.Context["path_parts"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"api", "v1", "pet"}, pathParts)
}

func TestPolicy_MethodToAction(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, "read"},
		{http.MethodPost, "create"},
		{http.MethodPatch, "update"},
		{http.MethodPut, "update"},
		{http.MethodDelete, "delete"},
		{"OPTIONS", "options"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			assert.Equal(t, tt.want, methodToAction(tt.method))
		})
	}
}

func TestPolicy_UserClaimsInSubject(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
	// Inject user claims into context (simulating Auth middleware).
	ctx := context.WithValue(req.Context(), UserContextKey, map[string]any{
		"sub":  "user-123",
		"role": "admin",
	})
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "user-123", eval.lastInput.Subject["sub"])
	assert.Equal(t, "admin", eval.lastInput.Subject["role"])
}

func TestPolicy_NoUserClaimsEmptySubject(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, eval.lastInput.Subject)
}

func TestPolicy_CustomInputBuilder(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	handler := Policy(eval, WithInputBuilder(func(r *http.Request) *port.PolicyInput {
		return &port.PolicyInput{
			Action:     "custom-action",
			SchemaName: "custom-schema",
			Subject:    map[string]any{"custom": true},
		}
	}))(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "custom-action", eval.lastInput.Action)
	assert.Equal(t, "custom-schema", eval.lastInput.SchemaName)
	assert.Equal(t, true, eval.lastInput.Subject["custom"])
}

func TestPolicy_DecisionInContext(t *testing.T) {
	eval := &mockEvaluator{allowed: true}
	var capturedDecision *port.PolicyDecision

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := PolicyDecisionFromContext(r.Context())
		if ok {
			capturedDecision = d
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := Policy(eval)(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedDecision)
	assert.True(t, capturedDecision.Allowed)
}

func TestPolicy_NoDecisionOnDeny(t *testing.T) {
	eval := &mockEvaluator{allowed: false, reason: "denied"}
	handler := Policy(eval)(newPolicyTestHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}
