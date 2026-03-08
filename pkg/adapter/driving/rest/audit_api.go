package rest

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// AuditAPI provides HTTP endpoints for querying the audit trail.
type AuditAPI struct {
	store port.AuditStore
}

// NewAuditAPI constructs an AuditAPI backed by the given store.
func NewAuditAPI(store port.AuditStore) *AuditAPI {
	return &AuditAPI{store: store}
}

// Routes returns a Chi router for audit endpoints.
func (a *AuditAPI) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/entity/{entityID}", a.GetEntityHistory)
	r.Get("/user/{userID}", a.GetUserActivity)
	return r
}

// GetEntityHistory returns audit trail entries for a specific entity.
func (a *AuditAPI) GetEntityHistory(w http.ResponseWriter, r *http.Request) {
	entityID := chi.URLParam(r, "entityID")
	if entityID == "" {
		WriteError(w, coreerrors.BadRequest("entityID is required"))
		return
	}

	opts := parseAuditOpts(r)
	entries, err := a.store.GetHistory(r.Context(), entityID, opts...)
	if err != nil {
		WriteError(w, coreerrors.Internal("failed to query audit history", err))
		return
	}

	WriteSuccess(w, entries)
}

// GetUserActivity returns audit trail entries for a specific user.
func (a *AuditAPI) GetUserActivity(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if userID == "" {
		WriteError(w, coreerrors.BadRequest("userID is required"))
		return
	}

	opts := parseAuditOpts(r)
	entries, err := a.store.GetUserActivity(r.Context(), userID, opts...)
	if err != nil {
		WriteError(w, coreerrors.Internal("failed to query user activity", err))
		return
	}

	WriteSuccess(w, entries)
}

func parseAuditOpts(r *http.Request) []port.AuditQueryOption {
	var opts []port.AuditQueryOption

	if schema := r.URL.Query().Get("schema"); schema != "" {
		opts = append(opts, port.WithAuditSchemaName(schema))
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			opts = append(opts, port.WithAuditLimit(n))
		}
	}

	return opts
}
