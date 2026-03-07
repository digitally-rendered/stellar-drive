package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

func TestRemoteOPA_BooleanAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/data/authz/allow", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		input, _ := body["input"].(map[string]any)
		assert.Equal(t, "create", input["action"])
		assert.Equal(t, "pet", input["schema_name"])

		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "create",
		SchemaName: "pet",
	})

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, reason)
}

func TestRemoteOPA_BooleanDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": false})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:     "delete",
		SchemaName: "pet",
	})

	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Contains(t, reason, "policy denied")
}

func TestRemoteOPA_DictResultAllow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"allow":  true,
				"reason": "",
			},
		})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRemoteOPA_DictResultDenyWithReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"allow":  false,
				"reason": "admin role required",
			},
		})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "delete"})

	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, "admin role required", reason)
}

func TestRemoteOPA_NilResultDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, reason, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Contains(t, reason, "nil result")
}

func TestRemoteOPA_DotToSlashConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/data/app/authz/allow", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(
		WithOPAURL(srv.URL),
		WithDefaultPolicy("app.authz.allow"),
	)
	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRemoteOPA_Non200Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal_error"}`))
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	_, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestRemoteOPA_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	_, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestRemoteOPA_ConnectionError(t *testing.T) {
	e := NewRemoteOPAEvaluator(
		WithOPAURL("http://127.0.0.1:1"), // nothing listening
		WithOPATimeout(100*time.Millisecond),
	)
	_, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote opa")
}

func TestRemoteOPA_CustomHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer srv.Close()

	customClient := &http.Client{Timeout: 1 * time.Second}
	e := NewRemoteOPAEvaluator(
		WithOPAURL(srv.URL),
		WithHTTPClient(customClient),
	)

	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{Action: "read"})
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRemoteOPA_LoadPolicy(t *testing.T) {
	e := NewRemoteOPAEvaluator()

	err := e.LoadPolicy(context.Background(), "authz", []byte("package authz"))
	require.NoError(t, err)

	e.mu.RLock()
	stored := e.policies["authz"]
	e.mu.RUnlock()
	assert.Equal(t, "package authz", string(stored))
}

func TestRemoteOPA_LoadPolicy_EmptyName(t *testing.T) {
	e := NewRemoteOPAEvaluator()
	err := e.LoadPolicy(context.Background(), "", []byte("data"))
	assert.Error(t, err)
}

func TestRemoteOPA_SubjectPassedThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		input, _ := body["input"].(map[string]any)
		subject, _ := input["subject"].(map[string]any)
		assert.Equal(t, "admin", subject["role"])
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer srv.Close()

	e := NewRemoteOPAEvaluator(WithOPAURL(srv.URL))
	allowed, _, err := e.Evaluate(context.Background(), &port.PolicyInput{
		Action:  "delete",
		Subject: map[string]any{"role": "admin"},
	})

	require.NoError(t, err)
	assert.True(t, allowed)
}
