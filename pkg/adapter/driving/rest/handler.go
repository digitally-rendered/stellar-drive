package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/registry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// restChannel is the fixed transport channel identifier for REST handlers.
const restChannel = "rest"

// HandlerOption is a functional option for configuring a GenericHandler.
type HandlerOption func(*GenericHandler)

// WithFunctionRegistry injects a FunctionRegistry into the handler, enabling
// custom handler overrides, guards, validators, and transforms.
func WithFunctionRegistry(reg *registry.FunctionRegistry) HandlerOption {
	return func(h *GenericHandler) {
		h.funcReg = reg
	}
}

// GenericHandler provides auto-generated CRUD HTTP endpoints for a single
// named schema. It delegates all business logic to the port.Service and uses
// the response/request helpers in this package for consistent serialisation.
//
// When a FunctionRegistry is provided via WithFunctionRegistry, the handler
// executes a resolution pipeline before each operation:
//  1. Handler override — if resolved, bypasses the entire default pipeline
//  2. Guards — all matching guards are evaluated; first error aborts (403)
//  3. Validators — all matching validators run on input data; first error aborts (400)
//  4. Transforms — all matching transforms mutate input data in order
//  5. Service call — default business logic (schema validation, events, repo)
type GenericHandler struct {
	schemaName string
	service    port.Service
	schemaReg  *schema.Registry
	funcReg    *registry.FunctionRegistry
}

// NewGenericHandler constructs a GenericHandler for the given schema.
func NewGenericHandler(schemaName string, service port.Service, schemaReg *schema.Registry, opts ...HandlerOption) *GenericHandler {
	h := &GenericHandler{
		schemaName: schemaName,
		service:    service,
		schemaReg:  schemaReg,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Routes returns a chi.Router with all CRUD routes mounted, including the
// three /_bulk endpoints. Bulk routes are registered before /{entityID} so
// that the literal path segment "_bulk" does not get captured as an entity ID.
func (h *GenericHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Post("/_bulk", h.BulkCreate)
	r.Patch("/_bulk", h.BulkUpdate)
	r.Delete("/_bulk", h.BulkDelete)
	r.Get("/{entityID}", h.GetByID)
	r.Patch("/{entityID}", h.Update)
	r.Delete("/{entityID}", h.Delete)
	return r
}

// Create handles POST /{schema}/
// Decodes the JSON body, runs the override pipeline (handler → guards →
// validators → transforms), delegates to the service, and returns 201 Created.
func (h *GenericHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpCreate, w, r) {
		return
	}
	if err := h.runGuards(registry.OpCreate, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	data, err := DecodeBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	if err := h.runValidators(registry.OpCreate, r.Context(), data); err != nil {
		WriteError(w, err)
		return
	}

	data, err = h.runTransforms(registry.OpCreate, r.Context(), data)
	if err != nil {
		WriteError(w, err)
		return
	}

	doc, err := h.service.Create(r.Context(), h.schemaName, data)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteCreated(w, doc)
}

// List handles GET /{schema}/
// Parses query params into a query.Query, delegates to the service, and returns
// a paginated list envelope. Guards are evaluated but validators/transforms do
// not apply (no input body).
func (h *GenericHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpList, w, r) {
		return
	}
	if err := h.runGuards(registry.OpList, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	q, err := ParseQueryParams(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	// Apply normalised pagination limits when they were extracted from the
	// query string.
	pagination := ParsePagination(r)
	if q.Limit == 0 {
		q.Limit = pagination.Limit
	}
	if q.Offset == 0 {
		q.Offset = pagination.Offset
	}
	if q.Cursor == "" {
		q.Cursor = pagination.Cursor
	}

	result, err := h.service.List(r.Context(), h.schemaName, q)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteList(w, result)
}

// GetByID handles GET /{schema}/{entityID}
// Extracts the entity ID from the URL, delegates to the service, and returns
// 200 OK on success or an appropriate error status. Supports ?fields= for
// field projection. Guards are evaluated but validators/transforms do not
// apply (no input body).
func (h *GenericHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpGet, w, r) {
		return
	}
	if err := h.runGuards(registry.OpGet, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	entityID := chi.URLParam(r, "entityID")
	if entityID == "" {
		WriteError(w, coreerrors.BadRequest("entityID is required"))
		return
	}

	doc, err := h.service.FindByID(r.Context(), h.schemaName, entityID)
	if err != nil {
		WriteError(w, err)
		return
	}

	// Apply field projection if ?fields= is specified.
	if fieldsStr := r.URL.Query().Get("fields"); fieldsStr != "" {
		fields := parseFields(fieldsStr)
		doc.Data = model.ProjectData(doc.Data, fields)
	}

	WriteSuccess(w, doc)
}

// Update handles PATCH /{schema}/{entityID}
// Decodes the JSON patch body, runs the override pipeline (handler → guards →
// validators → transforms), delegates to the service, and returns the updated
// document on success.
func (h *GenericHandler) Update(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpUpdate, w, r) {
		return
	}
	if err := h.runGuards(registry.OpUpdate, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	entityID := chi.URLParam(r, "entityID")
	if entityID == "" {
		WriteError(w, coreerrors.BadRequest("entityID is required"))
		return
	}

	data, err := DecodeBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	if err := h.runValidators(registry.OpUpdate, r.Context(), data); err != nil {
		WriteError(w, err)
		return
	}

	data, err = h.runTransforms(registry.OpUpdate, r.Context(), data)
	if err != nil {
		WriteError(w, err)
		return
	}

	doc, err := h.service.Update(r.Context(), h.schemaName, entityID, data)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteSuccess(w, doc)
}

// Delete handles DELETE /{schema}/{entityID}
// Soft-deletes the entity via the service and returns 204 No Content. Guards
// are evaluated but validators/transforms do not apply (no input body).
func (h *GenericHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpDelete, w, r) {
		return
	}
	if err := h.runGuards(registry.OpDelete, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	entityID := chi.URLParam(r, "entityID")
	if entityID == "" {
		WriteError(w, coreerrors.BadRequest("entityID is required"))
		return
	}

	if err := h.service.Delete(r.Context(), h.schemaName, entityID); err != nil {
		WriteError(w, err)
		return
	}

	WriteNoContent(w)
}

// BulkCreate handles POST /{schema}/_bulk
// Decodes the JSON array body, runs the override pipeline (handler → guards),
// delegates to the service, and returns 201 Created with a BulkOperationResult
// that describes the number of succeeded/failed items and the created documents.
func (h *GenericHandler) BulkCreate(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpBulkCreate, w, r) {
		return
	}
	if err := h.runGuards(registry.OpBulkCreate, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	items, err := DecodeBulkBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}
	if len(items) == 0 {
		WriteError(w, coreerrors.BadRequest("request body must contain at least one item"))
		return
	}

	docs, err := h.service.BulkCreate(r.Context(), h.schemaName, items)
	if err != nil {
		WriteError(w, err)
		return
	}

	result := model.BulkOperationResult{
		Succeeded: len(docs),
		Failed:    len(items) - len(docs),
		Items:     docs,
	}
	WriteCreated(w, result)
}

// BulkUpdate handles PATCH /{schema}/_bulk
// Decodes the JSON array body of BulkUpdateItem objects, runs the override
// pipeline (handler → guards), delegates to the service, and returns 200 OK
// with a BulkOperationResult containing the updated documents.
func (h *GenericHandler) BulkUpdate(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpBulkUpdate, w, r) {
		return
	}
	if err := h.runGuards(registry.OpBulkUpdate, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	items, err := DecodeBulkUpdateBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}
	if len(items) == 0 {
		WriteError(w, coreerrors.BadRequest("request body must contain at least one item"))
		return
	}

	docs, err := h.service.BulkUpdate(r.Context(), h.schemaName, items)
	if err != nil {
		WriteError(w, err)
		return
	}

	result := model.BulkOperationResult{
		Succeeded: len(docs),
		Failed:    len(items) - len(docs),
		Items:     docs,
	}
	WriteSuccess(w, result)
}

// BulkDelete handles DELETE /{schema}/_bulk
// Decodes the JSON object body with an "ids" array, runs the override pipeline
// (handler → guards), delegates to the service, and returns 204 No Content.
func (h *GenericHandler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	if h.checkOverride(registry.OpBulkDelete, w, r) {
		return
	}
	if err := h.runGuards(registry.OpBulkDelete, r.Context(), r); err != nil {
		WriteError(w, err)
		return
	}

	ids, err := DecodeBulkDeleteBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
		return
	}

	if err := h.service.BulkDelete(r.Context(), h.schemaName, ids); err != nil {
		WriteError(w, err)
		return
	}

	WriteNoContent(w)
}

// ---------------------------------------------------------------------------
// FunctionRegistry pipeline helpers
// ---------------------------------------------------------------------------

// schemaVersion returns the current version of the handler's schema from the
// registry, or an empty string if the schema is not registered.
func (h *GenericHandler) schemaVersion() string {
	env, err := h.schemaReg.GetEnvelope(h.schemaName, "")
	if err != nil {
		return ""
	}
	return env.Version
}

// checkOverride resolves a handler override for the given operation. If an
// override is found, it is called directly and true is returned; the caller
// should return immediately. When no override exists, false is returned.
func (h *GenericHandler) checkOverride(op registry.Operation, w http.ResponseWriter, r *http.Request) bool {
	if h.funcReg == nil {
		return false
	}
	if override, ok := h.funcReg.ResolveHandler(h.schemaName, op, h.schemaVersion(), restChannel); ok {
		override(w, r)
		return true
	}
	return false
}

// runGuards resolves and evaluates all guards for the given operation. Returns
// a Forbidden error on the first guard failure, or nil if all guards pass.
func (h *GenericHandler) runGuards(op registry.Operation, ctx context.Context, r *http.Request) error {
	if h.funcReg == nil {
		return nil
	}
	for _, guard := range h.funcReg.ResolveGuards(h.schemaName, op, h.schemaVersion(), restChannel) {
		if err := guard(ctx, r); err != nil {
			return coreerrors.Forbidden(err.Error())
		}
	}
	return nil
}

// runValidators resolves and evaluates all validators for the given operation.
// Returns a BadRequest error on the first validation failure.
func (h *GenericHandler) runValidators(op registry.Operation, ctx context.Context, data map[string]any) error {
	if h.funcReg == nil {
		return nil
	}
	for _, v := range h.funcReg.ResolveValidators(h.schemaName, op, h.schemaVersion(), restChannel) {
		if err := v(ctx, h.schemaName, data); err != nil {
			return coreerrors.BadRequest(err.Error())
		}
	}
	return nil
}

// runTransforms resolves and applies all transforms for the given operation.
// Each transform may mutate the data map; the final result is returned.
func (h *GenericHandler) runTransforms(op registry.Operation, ctx context.Context, data map[string]any) (map[string]any, error) {
	if h.funcReg == nil {
		return data, nil
	}
	for _, t := range h.funcReg.ResolveTransforms(h.schemaName, op, h.schemaVersion(), restChannel) {
		transformed, err := t(ctx, h.schemaName, data)
		if err != nil {
			return nil, err
		}
		if transformed != nil {
			data = transformed
		}
	}
	return data, nil
}

// parseFields splits a comma-separated fields string into trimmed field names.
func parseFields(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
