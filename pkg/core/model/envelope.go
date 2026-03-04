package model

// ResponseEnvelope wraps all API responses.
type ResponseEnvelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
	Meta    *MetaBody  `json:"meta,omitempty"`
}

// ErrorBody carries structured error information in a response envelope.
type ErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail describes a single field-level or item-level validation failure.
type ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// MetaBody carries pagination and collection metadata in a response envelope.
type MetaBody struct {
	Total   int64  `json:"total,omitempty"`
	HasMore bool   `json:"has_more,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Page    int    `json:"page,omitempty"`
	PerPage int    `json:"per_page,omitempty"`
}

// NewSuccessResponse creates a success envelope wrapping data.
func NewSuccessResponse(data any) *ResponseEnvelope {
	return &ResponseEnvelope{
		Success: true,
		Data:    data,
	}
}

// NewListResponse creates a success envelope with pagination metadata derived
// from a ListResult.
func NewListResponse(result *ListResult) *ResponseEnvelope {
	return &ResponseEnvelope{
		Success: true,
		Data:    result.Items,
		Meta: &MetaBody{
			Total:   result.Total,
			HasMore: result.HasMore,
			Cursor:  result.Cursor,
		},
	}
}

// NewErrorResponse creates an error envelope. details is optional.
func NewErrorResponse(code, message string, details ...ErrorDetail) *ResponseEnvelope {
	body := &ErrorBody{
		Code:    code,
		Message: message,
	}
	if len(details) > 0 {
		body.Details = details
	}
	return &ResponseEnvelope{
		Success: false,
		Error:   body,
	}
}
