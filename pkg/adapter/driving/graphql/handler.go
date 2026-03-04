package graphql

import (
	"encoding/json"
	"net/http"

	gql "github.com/graphql-go/graphql"
)

// graphqlRequest is the decoded body (or equivalent GET params) for a GraphQL
// HTTP request.
type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

// graphqlResponse is the standard GraphQL JSON response envelope.
type graphqlResponse struct {
	Data   any              `json:"data"`
	Errors []graphqlRespErr `json:"errors,omitempty"`
}

// graphqlRespErr carries a single error entry in the GraphQL response.
type graphqlRespErr struct {
	Message string `json:"message"`
}

// Handler serves GraphQL requests over HTTP. It accepts both POST (JSON body)
// and GET (query string) requests and returns the standard
// {"data": {...}, "errors": [...]} JSON envelope.
type Handler struct {
	schema gql.Schema
}

// NewHandler constructs a Handler for the given compiled GraphQL schema.
func NewHandler(schema gql.Schema) *Handler {
	return &Handler{schema: schema}
}

// ServeHTTP dispatches GET and POST requests to the GraphQL executor.
//
//   - POST: expects a JSON body {"query":"...","variables":{...},"operationName":"..."}
//   - GET:  expects URL query parameters ?query=...&variables=...&operationName=...
//
// All other methods return 405 Method Not Allowed.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req graphqlRequest

	switch r.Method {
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeGraphQLError(w, http.StatusBadRequest, "could not decode request body: "+err.Error())
			return
		}

	case http.MethodGet:
		q := r.URL.Query()
		req.Query = q.Get("query")
		req.OperationName = q.Get("operationName")

		if varsJSON := q.Get("variables"); varsJSON != "" {
			if err := json.Unmarshal([]byte(varsJSON), &req.Variables); err != nil {
				writeGraphQLError(w, http.StatusBadRequest, "could not decode variables: "+err.Error())
				return
			}
		}

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if req.Query == "" {
		writeGraphQLError(w, http.StatusBadRequest, "query is required")
		return
	}

	result := gql.Do(gql.Params{
		Schema:         h.schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        r.Context(),
	})

	resp := graphqlResponse{Data: result.Data}
	for _, e := range result.Errors {
		resp.Errors = append(resp.Errors, graphqlRespErr{Message: e.Error()})
	}

	status := http.StatusOK
	if len(result.Errors) > 0 && result.Data == nil {
		// All top-level errors with no partial data — treat as a server error.
		status = http.StatusBadRequest
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// writeGraphQLError writes a GraphQL-shaped error response without executing
// the schema. Used for request-parsing failures before execution begins.
func writeGraphQLError(w http.ResponseWriter, statusCode int, message string) {
	resp := graphqlResponse{
		Errors: []graphqlRespErr{{Message: message}},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(resp)
}
