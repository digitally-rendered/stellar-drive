package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// Mock service
// ---------------------------------------------------------------------------

type mockService struct {
	createFn      func(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error)
	findByIDFn    func(ctx context.Context, schemaName string, entityID string) (*model.Document, error)
	listFn        func(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)
	updateFn      func(ctx context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error)
	deleteFn      func(ctx context.Context, schemaName string, entityID string) error
	bulkCreateFn  func(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error)
	bulkUpdateFn  func(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error)
	bulkDeleteFn  func(ctx context.Context, schemaName string, ids []string) error
}

func (m *mockService) Create(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error) {
	if m.createFn != nil {
		return m.createFn(ctx, schemaName, input)
	}
	return &model.Document{EntityID: "new-1", Data: input}, nil
}

func (m *mockService) FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, schemaName, entityID)
	}
	return &model.Document{EntityID: entityID, Data: map[string]any{"name": "test"}}, nil
}

func (m *mockService) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	if m.listFn != nil {
		return m.listFn(ctx, schemaName, q)
	}
	return &model.ListResult{
		Items: []*model.Document{{EntityID: "1", Data: map[string]any{"name": "a"}}},
		Total: 1,
	}, nil
}

func (m *mockService) Update(ctx context.Context, schemaName string, entityID string, input map[string]any) (*model.Document, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, schemaName, entityID, input)
	}
	return &model.Document{EntityID: entityID, Data: input}, nil
}

func (m *mockService) Delete(ctx context.Context, schemaName string, entityID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, schemaName, entityID)
	}
	return nil
}

func (m *mockService) BulkCreate(ctx context.Context, schemaName string, inputs []map[string]any) ([]*model.Document, error) {
	if m.bulkCreateFn != nil {
		return m.bulkCreateFn(ctx, schemaName, inputs)
	}
	docs := make([]*model.Document, len(inputs))
	for i, input := range inputs {
		docs[i] = &model.Document{EntityID: fmt.Sprintf("bulk-%d", i+1), Data: input}
	}
	return docs, nil
}

func (m *mockService) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	if m.bulkUpdateFn != nil {
		return m.bulkUpdateFn(ctx, schemaName, items)
	}
	docs := make([]*model.Document, len(items))
	for i, item := range items {
		docs[i] = &model.Document{EntityID: item.EntityID, Data: item.Data}
	}
	return docs, nil
}

func (m *mockService) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	if m.bulkDeleteFn != nil {
		return m.bulkDeleteFn(ctx, schemaName, ids)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestHandler(funcReg *registry.FunctionRegistry) *GenericHandler {
	reg := schema.NewRegistry()
	_, _ = reg.Register(&schema.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
	})

	svc := &mockService{}
	var opts []HandlerOption
	if funcReg != nil {
		opts = append(opts, WithFunctionRegistry(funcReg))
	}
	return NewGenericHandler("pet", svc, reg, opts...)
}

func newTestRouter(h *GenericHandler) chi.Router {
	r := chi.NewRouter()
	r.Mount("/pet", h.Routes())
	return r
}

func doRequest(r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// Tests: No FunctionRegistry (baseline — original behaviour)
// ---------------------------------------------------------------------------

func TestGenericHandler_NoFuncReg_Create(t *testing.T) {
	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})
	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestGenericHandler_NoFuncReg_GetByID(t *testing.T) {
	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodGet, "/pet/abc-123", nil)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// ---------------------------------------------------------------------------
// Tests: Handler override
// ---------------------------------------------------------------------------

func TestGenericHandler_HandlerOverride(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterHandler("pet", registry.OpCreate, registry.Scope{}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"custom":"override"}`))
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.Contains(t, rr.Body.String(), "custom")
}

func TestGenericHandler_HandlerOverride_ScopedByChannel(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	// Register override only for "rest" channel.
	funcReg.RegisterHandler("pet", registry.OpGet, registry.Scope{Channel: "rest"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodGet, "/pet/abc-123", nil)

	assert.Equal(t, http.StatusTeapot, rr.Code)
}

// ---------------------------------------------------------------------------
// Tests: Guards
// ---------------------------------------------------------------------------

func TestGenericHandler_Guard_Passes(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterGuard("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		return nil // allow
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestGenericHandler_Guard_Blocks(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterGuard("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		return fmt.Errorf("admin access required")
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "admin access required")
}

func TestGenericHandler_Guard_BlocksRead(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterGuard("pet", registry.OpGet, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		return fmt.Errorf("not allowed")
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodGet, "/pet/abc-123", nil)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestGenericHandler_Guard_BlocksDelete(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterGuard("pet", registry.OpDelete, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		return fmt.Errorf("cannot delete")
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodDelete, "/pet/abc-123", nil)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// ---------------------------------------------------------------------------
// Tests: Validators
// ---------------------------------------------------------------------------

func TestGenericHandler_Validator_Passes(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
		return nil
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestGenericHandler_Validator_Rejects(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
		if _, ok := data["name"]; !ok {
			return fmt.Errorf("name is required")
		}
		return nil
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"status": "available"})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "name is required")
}

func TestGenericHandler_Validator_RejectsUpdate(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterValidator("pet", registry.OpUpdate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
		if v, ok := data["status"]; ok && v == "invalid" {
			return fmt.Errorf("invalid status value")
		}
		return nil
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPatch, "/pet/abc-123", map[string]any{"status": "invalid"})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid status value")
}

// ---------------------------------------------------------------------------
// Tests: Transforms
// ---------------------------------------------------------------------------

func TestGenericHandler_Transform_MutatesData(t *testing.T) {
	var capturedData map[string]any

	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
		data["injected"] = true
		return data, nil
	})

	svc := &mockService{
		createFn: func(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error) {
			capturedData = input
			return &model.Document{EntityID: "new-1", Data: input}, nil
		},
	}

	reg := schema.NewRegistry()
	_, _ = reg.Register(&schema.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
	})

	h := NewGenericHandler("pet", svc, reg, WithFunctionRegistry(funcReg))
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	require.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, true, capturedData["injected"])
	assert.Equal(t, "Fido", capturedData["name"])
}

func TestGenericHandler_Transform_Error(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
		return nil, fmt.Errorf("transform failed")
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// ---------------------------------------------------------------------------
// Tests: Full pipeline (guard + validator + transform)
// ---------------------------------------------------------------------------

func TestGenericHandler_FullPipeline(t *testing.T) {
	var capturedData map[string]any
	var pipelineOrder []string

	funcReg := registry.NewFunctionRegistry()

	// Guard that passes.
	funcReg.RegisterGuard("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		pipelineOrder = append(pipelineOrder, "guard")
		return nil
	})

	// Validator that passes.
	funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
		pipelineOrder = append(pipelineOrder, "validator")
		return nil
	})

	// Transform that adds a field.
	funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
		pipelineOrder = append(pipelineOrder, "transform")
		data["transformed"] = true
		return data, nil
	})

	svc := &mockService{
		createFn: func(ctx context.Context, schemaName string, input map[string]any) (*model.Document, error) {
			capturedData = input
			return &model.Document{EntityID: "new-1", Data: input}, nil
		},
	}

	reg := schema.NewRegistry()
	_, _ = reg.Register(&schema.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
	})

	h := NewGenericHandler("pet", svc, reg, WithFunctionRegistry(funcReg))
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	require.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, []string{"guard", "validator", "transform"}, pipelineOrder)
	assert.Equal(t, true, capturedData["transformed"])
}

// ---------------------------------------------------------------------------
// Tests: Handler override bypasses guards/validators/transforms
// ---------------------------------------------------------------------------

func TestGenericHandler_Override_BypassesPipeline(t *testing.T) {
	guardCalled := false
	validatorCalled := false

	funcReg := registry.NewFunctionRegistry()

	funcReg.RegisterHandler("pet", registry.OpCreate, registry.Scope{}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	funcReg.RegisterGuard("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		guardCalled = true
		return nil
	})

	funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{}, func(ctx context.Context, schemaName string, data map[string]any) error {
		validatorCalled = true
		return nil
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/", map[string]any{"name": "Fido"})

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.False(t, guardCalled, "guard should not be called when handler override is active")
	assert.False(t, validatorCalled, "validator should not be called when handler override is active")
}

// ---------------------------------------------------------------------------
// Tests: Bulk endpoints
// ---------------------------------------------------------------------------

func TestBulkCreate_Success(t *testing.T) {
	inputs := []map[string]any{
		{"name": "Fido"},
		{"name": "Rex"},
	}

	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/_bulk", inputs)

	require.Equal(t, http.StatusCreated, rr.Code)

	var resp struct {
		Data struct {
			Succeeded int              `json:"succeeded"`
			Failed    int              `json:"failed"`
			Items     []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Data.Succeeded)
	assert.Equal(t, 0, resp.Data.Failed)
	assert.Len(t, resp.Data.Items, 2)
}

func TestBulkCreate_EmptyBody_400(t *testing.T) {
	// Send an empty JSON array — must be rejected with 400.
	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/_bulk", []map[string]any{})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "at least one item")
}

func TestBulkUpdate_Success(t *testing.T) {
	items := []model.BulkUpdateItem{
		{EntityID: "ent-1", Data: map[string]any{"status": "sold"}},
		{EntityID: "ent-2", Data: map[string]any{"status": "available"}},
	}

	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPatch, "/pet/_bulk", items)

	require.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Data struct {
			Succeeded int              `json:"succeeded"`
			Failed    int              `json:"failed"`
			Items     []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Data.Succeeded)
	assert.Equal(t, 0, resp.Data.Failed)
	assert.Len(t, resp.Data.Items, 2)
}

func TestBulkDelete_Success(t *testing.T) {
	payload := map[string]any{"ids": []string{"ent-1", "ent-2", "ent-3"}}

	h := newTestHandler(nil)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodDelete, "/pet/_bulk", payload)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Empty(t, rr.Body.String())
}

func TestBulkCreate_GuardBlocks(t *testing.T) {
	funcReg := registry.NewFunctionRegistry()
	funcReg.RegisterGuard("pet", registry.OpBulkCreate, registry.Scope{}, func(ctx context.Context, r *http.Request) error {
		return fmt.Errorf("bulk write not permitted")
	})

	h := newTestHandler(funcReg)
	r := newTestRouter(h)
	rr := doRequest(r, http.MethodPost, "/pet/_bulk", []map[string]any{{"name": "Fido"}})

	assert.Equal(t, http.StatusForbidden, rr.Code)
	assert.Contains(t, rr.Body.String(), "bulk write not permitted")
}
