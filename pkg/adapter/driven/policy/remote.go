package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// RemoteOPAOption configures a RemoteOPAEvaluator.
type RemoteOPAOption func(*RemoteOPAEvaluator)

// WithOPAURL sets the base URL of the OPA server (default: http://localhost:8181).
func WithOPAURL(url string) RemoteOPAOption {
	return func(e *RemoteOPAEvaluator) {
		e.baseURL = strings.TrimRight(url, "/")
	}
}

// WithDefaultPolicy sets the default policy path (default: "authz/allow").
// Dots in the path are converted to slashes when building the OPA URL.
func WithDefaultPolicy(path string) RemoteOPAOption {
	return func(e *RemoteOPAEvaluator) {
		e.defaultPolicy = path
	}
}

// WithOPATimeout sets the HTTP request timeout (default: 5s).
func WithOPATimeout(d time.Duration) RemoteOPAOption {
	return func(e *RemoteOPAEvaluator) {
		e.timeout = d
	}
}

// WithHTTPClient overrides the default HTTP client. Useful for testing.
func WithHTTPClient(client *http.Client) RemoteOPAOption {
	return func(e *RemoteOPAEvaluator) {
		e.client = client
	}
}

// RemoteOPAEvaluator implements port.PolicyEvaluator by calling a remote OPA
// server via its REST API. It POSTs {"input": ...} to /v1/data/{policy_path}
// and interprets the response.
//
// This is the Go equivalent of Python's OpaRemotePolicy.
type RemoteOPAEvaluator struct {
	baseURL       string
	defaultPolicy string
	timeout       time.Duration
	client        *http.Client

	mu       sync.RWMutex
	policies map[string][]byte
}

// NewRemoteOPAEvaluator creates a new evaluator that connects to a remote
// OPA server. Configure with functional options.
func NewRemoteOPAEvaluator(opts ...RemoteOPAOption) *RemoteOPAEvaluator {
	e := &RemoteOPAEvaluator{
		baseURL:       "http://localhost:8181",
		defaultPolicy: "authz/allow",
		timeout:       5 * time.Second,
		policies:      make(map[string][]byte),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.client == nil {
		e.client = &http.Client{Timeout: e.timeout}
	}
	return e
}

// Evaluate sends the PolicyInput to the remote OPA server and interprets the
// result. The default policy path is used for evaluation.
func (e *RemoteOPAEvaluator) Evaluate(ctx context.Context, input *port.PolicyInput) (bool, string, error) {
	opaInput := map[string]any{
		"action":      input.Action,
		"schema_name": input.SchemaName,
		"resource":    input.Resource,
		"subject":     input.Subject,
		"context":     input.Context,
	}

	result, err := e.evaluateRaw(ctx, e.defaultPolicy, opaInput)
	if err != nil {
		return false, "", fmt.Errorf("remote opa: %w", err)
	}

	// OPA returns {"result": <value>} where value may be:
	//   - bool:   true/false
	//   - object: {"allow": true/false, "reason": "..."}
	//   - nil:    policy not found or returned undefined
	decision := result["result"]
	switch v := decision.(type) {
	case bool:
		if !v {
			return false, "policy denied by remote OPA", nil
		}
		return true, "", nil
	case map[string]any:
		allowed, _ := v["allow"].(bool)
		reason, _ := v["reason"].(string)
		if !allowed && reason == "" {
			reason = "policy denied by remote OPA"
		}
		return allowed, reason, nil
	default:
		if decision == nil {
			return false, "policy returned nil result", nil
		}
		return true, "", nil
	}
}

// evaluateRaw POSTs to the OPA REST API and returns the parsed JSON response.
func (e *RemoteOPAEvaluator) evaluateRaw(ctx context.Context, policyPath string, input map[string]any) (map[string]any, error) {
	// Convert dots to slashes: "authz.allow" → "authz/allow"
	path := strings.ReplaceAll(policyPath, ".", "/")
	url := fmt.Sprintf("%s/v1/data/%s", e.baseURL, path)

	payload := map[string]any{"input": input}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opa request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opa returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// LoadPolicy stores the named policy document. For a remote evaluator,
// policies are managed on the OPA server; the bytes are retained for
// interface compliance.
func (e *RemoteOPAEvaluator) LoadPolicy(_ context.Context, name string, policy []byte) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("policy: LoadPolicy: name must not be empty")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	stored := make([]byte, len(policy))
	copy(stored, policy)
	e.policies[name] = stored
	return nil
}
