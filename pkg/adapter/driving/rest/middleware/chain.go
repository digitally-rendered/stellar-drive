package middleware

import "net/http"

// Chain builds an ordered middleware stack from the provided middleware
// functions and returns a single middleware function that applies them in
// left-to-right (outermost-first) order.
//
// Example:
//
//	stack := middleware.Chain(Recovery, RequestID, Logging, SecurityHeaders)
//	http.ListenAndServe(":8080", stack(router))
func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		// Apply in reverse so the first middleware in the list is the outermost
		// wrapper (i.e. it executes first on the way in and last on the way out).
		h := final
		for i := len(middlewares) - 1; i >= 0; i-- {
			h = middlewares[i](h)
		}
		return h
	}
}
