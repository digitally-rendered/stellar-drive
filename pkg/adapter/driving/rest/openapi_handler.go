package rest

import (
	"net/http"
	"sync"

	"github.com/digitally-rendered/stellar-drive/pkg/codegen/sdk"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// openAPICache holds a pre-rendered OpenAPI JSON document together with the
// registry size that was in effect when the document was generated. A change
// in the registry size (schemas added or removed) invalidates the cache.
type openAPICache struct {
	mu          sync.RWMutex
	payload     []byte
	registryLen int // number of schema names at last render
}

// get returns the cached payload when the registry still has the same number
// of schema names as when the cache was populated. It returns nil when the
// cache is stale or empty.
func (c *openAPICache) get(currentLen int) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.payload == nil || c.registryLen != currentLen {
		return nil
	}
	return c.payload
}

// set stores payload together with the registry size that produced it.
func (c *openAPICache) set(payload []byte, registryLen int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.payload = payload
	c.registryLen = registryLen
}

// NewOpenAPIHandler returns an http.HandlerFunc that serves the OpenAPI 3.0.3
// specification for all schemas currently registered in registry.
//
// When versioned is true the handler calls GenerateVersionedOpenAPI, which
// includes every registered schema version and adds versioned component
// schemas and paths. When false, only the latest version of each schema is
// included via GenerateOpenAPI.
//
// The rendered JSON document is cached in memory. The cache is invalidated
// automatically whenever the number of registered schema names changes (schemas
// were added or removed since the last render). This avoids re-generating the
// spec on every request without requiring a time-based TTL.
func NewOpenAPIHandler(registry *schema.Registry, title, version string, versioned bool) http.HandlerFunc {
	cache := &openAPICache{}

	return func(w http.ResponseWriter, r *http.Request) {
		currentLen := len(registry.Names())

		if payload := cache.get(currentLen); payload != nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payload)
			return
		}

		var (
			spec *sdk.OpenAPISpec
			err  error
		)

		if versioned {
			spec, err = sdk.GenerateVersionedOpenAPI(registry, title, version)
		} else {
			spec, err = sdk.GenerateOpenAPI(registry, title, version)
		}

		if err != nil {
			http.Error(w, `{"error":"failed to generate OpenAPI spec"}`, http.StatusInternalServerError)
			return
		}

		payload, err := sdk.MarshalJSON(spec)
		if err != nil {
			http.Error(w, `{"error":"failed to serialise OpenAPI spec"}`, http.StatusInternalServerError)
			return
		}

		cache.set(payload, currentLen)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}
}
