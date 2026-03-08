package sdtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// LifecycleRunner exercises the full CRUD lifecycle of a single schema against
// a running HTTP server. It is not opinionated about the assertion library;
// callers inspect the returned *LifecycleResult themselves.
//
//	runner := sdtest.NewLifecycleRunner(t, srv.URL)
//	result := runner.RunFull("pets", createPayload, updatePayload)
//	require.Equal(t, http.StatusCreated, result.Created.StatusCode)
type LifecycleRunner struct {
	t      *testing.T
	client *http.Client
	base   string
}

// NewLifecycleRunner returns a LifecycleRunner that targets baseURL (e.g. the
// URL returned by httptest.NewServer).
func NewLifecycleRunner(t *testing.T, baseURL string) *LifecycleRunner {
	t.Helper()
	return &LifecycleRunner{
		t:      t,
		client: &http.Client{},
		base:   baseURL,
	}
}

// LifecycleResult holds the HTTP responses from each step of RunFull. Callers
// assert on StatusCode, Body, and headers as needed. Each response body is
// fully buffered so it can be read multiple times.
type LifecycleResult struct {
	// Created is the response from POST /{schemaPath}/
	Created *http.Response

	// Retrieved is the response from GET /{schemaPath}/{entityID}
	Retrieved *http.Response

	// Listed is the response from GET /{schemaPath}/
	Listed *http.Response

	// Updated is the response from PATCH /{schemaPath}/{entityID}
	Updated *http.Response

	// Deleted is the response from DELETE /{schemaPath}/{entityID}
	Deleted *http.Response

	// NotFound is the response from GET /{schemaPath}/{entityID} after deletion
	NotFound *http.Response
}

// RunFull executes the full CRUD lifecycle in order:
//
//  1. POST   /{schemaPath}/          → createBody
//  2. GET    /{schemaPath}/{id}      — uses the entity_id from step 1
//  3. GET    /{schemaPath}/          — list
//  4. PATCH  /{schemaPath}/{id}      → updateBody
//  5. DELETE /{schemaPath}/{id}
//  6. GET    /{schemaPath}/{id}      — expect 404
//
// If the POST in step 1 does not return 201 Created, RunFull calls t.Fatalf
// and halts immediately because subsequent steps depend on the created entity
// ID. All other step failures are surfaced through the returned *LifecycleResult
// for the caller to assert on.
func (lr *LifecycleRunner) RunFull(schemaPath string, createBody, updateBody map[string]any) *LifecycleResult {
	lr.t.Helper()

	result := &LifecycleResult{}

	// Step 1: Create.
	result.Created = lr.post(schemaPath+"/", createBody)

	if result.Created.StatusCode != http.StatusCreated {
		lr.t.Fatalf("sdtest.LifecycleRunner.RunFull: POST %s returned %d, want 201",
			schemaPath, result.Created.StatusCode)
	}

	entityID := extractEntityID(lr.t, result.Created)

	// Step 2: Retrieve.
	result.Retrieved = lr.get(fmt.Sprintf("%s/%s", schemaPath, entityID))

	// Step 3: List.
	result.Listed = lr.get(schemaPath + "/")

	// Step 4: Update.
	result.Updated = lr.patch(fmt.Sprintf("%s/%s", schemaPath, entityID), updateBody)

	// Step 5: Delete.
	result.Deleted = lr.doDelete(fmt.Sprintf("%s/%s", schemaPath, entityID))

	// Step 6: Confirm 404.
	result.NotFound = lr.get(fmt.Sprintf("%s/%s", schemaPath, entityID))

	return result
}

// ---------------------------------------------------------------------------
// HTTP helper methods
// ---------------------------------------------------------------------------

func (lr *LifecycleRunner) post(path string, body map[string]any) *http.Response {
	lr.t.Helper()
	return lr.do(http.MethodPost, path, body)
}

func (lr *LifecycleRunner) get(path string) *http.Response {
	lr.t.Helper()
	return lr.do(http.MethodGet, path, nil)
}

func (lr *LifecycleRunner) patch(path string, body map[string]any) *http.Response {
	lr.t.Helper()
	return lr.do(http.MethodPatch, path, body)
}

func (lr *LifecycleRunner) doDelete(path string) *http.Response {
	lr.t.Helper()
	return lr.do(http.MethodDelete, path, nil)
}

// do executes an HTTP request and buffers the response body so it can be read
// multiple times by the caller. The caller is responsible for eventually
// closing the buffered response body (which is a no-op for bytes.Buffer but
// respects the io.ReadCloser contract).
func (lr *LifecycleRunner) do(method, path string, body map[string]any) *http.Response {
	lr.t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			lr.t.Fatalf("sdtest: marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, lr.base+"/"+path, reqBody)
	if err != nil {
		lr.t.Fatalf("sdtest: build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := lr.client.Do(req)
	if err != nil {
		lr.t.Fatalf("sdtest: execute request %s %s: %v", method, path, err)
	}

	// Buffer the body so callers can decode it without consuming the stream.
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck
	if err != nil {
		lr.t.Fatalf("sdtest: read response body %s %s: %v", method, path, err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))

	return resp
}

// ---------------------------------------------------------------------------
// Response parsing helpers
// ---------------------------------------------------------------------------

// extractEntityID decodes the JSON response from a Create call and extracts
// data.entity_id. It calls t.Fatalf on any parse failure.
func extractEntityID(t *testing.T, resp *http.Response) string {
	t.Helper()

	var envelope struct {
		Data struct {
			EntityID string `json:"entity_id"`
		} `json:"data"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("sdtest: read create response: %v", err)
	}

	// Restore the body for subsequent reads by the caller.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("sdtest: decode create response: %v\nbody: %s", err, body)
	}
	if envelope.Data.EntityID == "" {
		t.Fatalf("sdtest: create response missing data.entity_id\nbody: %s", body)
	}
	return envelope.Data.EntityID
}
