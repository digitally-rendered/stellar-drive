package rest

import (
	"encoding/json"
	"errors"
	"net/http"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
)

// WriteJSON writes a JSON response with the given status code and data payload.
// The Content-Type header is set to application/json before writing.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Ignore the encoding error — headers are already sent and there is no
	// meaningful recovery path at this point.
	_ = json.NewEncoder(w).Encode(data)
}

// WriteSuccess writes a 200 OK response containing a success envelope.
func WriteSuccess(w http.ResponseWriter, data any) {
	WriteJSON(w, http.StatusOK, model.NewSuccessResponse(data))
}

// WriteCreated writes a 201 Created response containing a success envelope.
func WriteCreated(w http.ResponseWriter, data any) {
	WriteJSON(w, http.StatusCreated, model.NewSuccessResponse(data))
}

// WriteList writes a 200 OK response containing a paginated list envelope
// derived from result.
func WriteList(w http.ResponseWriter, result *model.ListResult) {
	WriteJSON(w, http.StatusOK, model.NewListResponse(result))
}

// WriteError maps err to an appropriate HTTP status code and writes an error
// envelope. If err is (or wraps) a *errors.DomainError, its Code.HTTPStatus()
// is used for the status. Any other error produces a 500 Internal Server Error.
func WriteError(w http.ResponseWriter, err error) {
	var de *coreerrors.DomainError
	if errors.As(err, &de) {
		status := de.Code.HTTPStatus()
		details := make([]model.ErrorDetail, len(de.Details))
		for i, d := range de.Details {
			details[i] = model.ErrorDetail{
				Field:   d.Field,
				Message: d.Message,
				Code:    d.Code,
			}
		}
		WriteJSON(w, status, model.NewErrorResponse(string(de.Code), de.Message, details...))
		return
	}
	WriteJSON(w, http.StatusInternalServerError, model.NewErrorResponse(
		string(coreerrors.CodeInternal),
		"an unexpected error occurred",
	))
}

// WriteNoContent writes a 204 No Content response with no body.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
