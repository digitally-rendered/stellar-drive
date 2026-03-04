package rest

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// GenericHandler provides auto-generated CRUD HTTP endpoints for a single
// named schema. It delegates all business logic to the port.Service and uses
// the response/request helpers in this package for consistent serialisation.
type GenericHandler struct {
	schemaName string
	service    port.Service
	registry   *schema.Registry
}

// NewGenericHandler constructs a GenericHandler for the given schema.
func NewGenericHandler(schemaName string, service port.Service, registry *schema.Registry) *GenericHandler {
	return &GenericHandler{
		schemaName: schemaName,
		service:    service,
		registry:   registry,
	}
}

// Routes returns a chi.Router with all five CRUD routes mounted.
func (h *GenericHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{entityID}", h.GetByID)
	r.Patch("/{entityID}", h.Update)
	r.Delete("/{entityID}", h.Delete)
	return r
}

// Create handles POST /{schema}/
// Decodes the JSON body, delegates to the service, and returns 201 Created.
func (h *GenericHandler) Create(w http.ResponseWriter, r *http.Request) {
	data, err := DecodeBody(r)
	if err != nil {
		WriteError(w, coreerrors.BadRequest(err.Error()))
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
// a paginated list envelope.
func (h *GenericHandler) List(w http.ResponseWriter, r *http.Request) {
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
// 200 OK on success or an appropriate error status.
func (h *GenericHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	WriteSuccess(w, doc)
}

// Update handles PATCH /{schema}/{entityID}
// Decodes the JSON patch body, delegates to the service, and returns the
// updated document on success.
func (h *GenericHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	doc, err := h.service.Update(r.Context(), h.schemaName, entityID, data)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteSuccess(w, doc)
}

// Delete handles DELETE /{schema}/{entityID}
// Soft-deletes the entity via the service and returns 204 No Content.
func (h *GenericHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
