// Package ctxkey defines typed context keys shared between adapter and core
// layers without creating import cycles.
package ctxkey

// IfMatchKey is the context key for storing the If-Match header value.
// The ETag middleware sets this on inbound requests; the service layer
// reads it when publishing pre-events.
type IfMatchKey struct{}
