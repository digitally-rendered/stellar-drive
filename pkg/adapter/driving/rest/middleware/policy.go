package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// policyDecisionKey stores the policy decision in request context.
const policyDecisionKey contextKey = "policy_decision"

// PolicyDecisionFromContext retrieves the policy decision stored by the
// Policy middleware. Returns nil, false if no decision is present.
func PolicyDecisionFromContext(ctx context.Context) (*port.PolicyDecision, bool) {
	v := ctx.Value(policyDecisionKey)
	if v == nil {
		return nil, false
	}
	d, ok := v.(*port.PolicyDecision)
	return d, ok
}

// PolicyOption configures the Policy middleware.
type PolicyOption func(*policyMiddleware)

// WithSkipPaths sets URL path prefixes that bypass policy evaluation.
func WithSkipPaths(paths ...string) PolicyOption {
	return func(m *policyMiddleware) {
		m.skipPaths = paths
	}
}

// WithInputBuilder sets a custom function to build the PolicyInput from
// the HTTP request. When nil, the default builder is used.
func WithInputBuilder(fn func(r *http.Request) *port.PolicyInput) PolicyOption {
	return func(m *policyMiddleware) {
		m.buildInput = fn
	}
}

// policyMiddleware holds the configuration for the Policy middleware.
type policyMiddleware struct {
	evaluator  port.PolicyEvaluator
	skipPaths  []string
	buildInput func(r *http.Request) *port.PolicyInput
}

// Policy returns an HTTP middleware that evaluates every request against the
// provided PolicyEvaluator. Denied requests receive 403 Forbidden. Evaluation
// errors produce 503 Service Unavailable.
//
// The middleware should be placed after the Auth middleware in the chain so
// that user claims are available in the request context.
//
// Default skip paths: /health, /ready, /_topology, /docs, /openapi.json
func Policy(evaluator port.PolicyEvaluator, opts ...PolicyOption) func(http.Handler) http.Handler {
	m := &policyMiddleware{
		evaluator: evaluator,
		skipPaths: []string{
			"/health",
			"/ready",
			"/_topology",
			"/docs",
			"/openapi.json",
		},
	}
	for _, opt := range opts {
		opt(m)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip configured paths.
			for _, prefix := range m.skipPaths {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Build the policy input.
			var input *port.PolicyInput
			if m.buildInput != nil {
				input = m.buildInput(r)
			} else {
				input = defaultBuildInput(r)
			}

			// Evaluate.
			allowed, reason, err := m.evaluator.Evaluate(r.Context(), input)
			if err != nil {
				slog.ErrorContext(r.Context(), "policy evaluation error",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path,
				)
				writePolicyError(w, http.StatusServiceUnavailable,
					"INTERNAL", "policy service temporarily unavailable")
				return
			}

			if !allowed {
				slog.InfoContext(r.Context(), "policy denied",
					"method", r.Method,
					"path", r.URL.Path,
					"reason", reason,
				)
				writePolicyError(w, http.StatusForbidden, "FORBIDDEN", reason)
				return
			}

			// Store decision in context for downstream use.
			decision := &port.PolicyDecision{Allowed: true}
			ctx := context.WithValue(r.Context(), policyDecisionKey, decision)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// defaultBuildInput constructs a PolicyInput from the HTTP request and auth
// context. It derives the action from the HTTP method and attempts to extract
// the schema name from the URL path.
func defaultBuildInput(r *http.Request) *port.PolicyInput {
	action := methodToAction(r.Method)
	pathParts := splitPath(r.URL.Path)

	// Attempt to extract schema name from path.
	// Expected paths: /api/v1/{schemaName}[/{entityID}]
	schemaName := ""
	if len(pathParts) >= 3 {
		schemaName = pathParts[2]
	}

	// Get user claims from auth context (may be empty).
	subject := make(map[string]any)
	if claims, ok := UserFromContext(r.Context()); ok {
		subject = claims
	}

	// Build ambient context.
	reqCtx := map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"path_parts": pathParts,
	}
	if id := RequestIDFromContext(r.Context()); id != "" {
		reqCtx["request_id"] = id
	}

	return &port.PolicyInput{
		Action:     action,
		SchemaName: schemaName,
		Subject:    subject,
		Context:    reqCtx,
	}
}

// methodToAction maps HTTP methods to CRUD action names.
func methodToAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodGet:
		return "read"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

// splitPath splits a URL path into non-empty segments.
func splitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// writePolicyError writes a JSON error response for policy decisions. This is
// a self-contained helper because the middleware package cannot import the
// rest package (circular dependency).
func writePolicyError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}
