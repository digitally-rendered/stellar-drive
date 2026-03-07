package middleware

import (
	"bytes"
	"container/list"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheOption configures the Cache middleware.
type CacheOption func(*cacheConfig)

// cacheConfig holds the resolved configuration for the Cache middleware.
type cacheConfig struct {
	ttl        time.Duration
	maxEntries int
	skipPaths  []string
}

// WithCacheTTL sets the time-to-live for cached entries. Entries older than
// the TTL are treated as expired on next access (lazy expiration).
// Default: 60 seconds.
func WithCacheTTL(d time.Duration) CacheOption {
	return func(c *cacheConfig) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithMaxCacheEntries sets the maximum number of entries held in the LRU
// cache. When the limit is reached, the least-recently-used entry is evicted
// to make room for the incoming entry.
// Default: 1000.
func WithMaxCacheEntries(n int) CacheOption {
	return func(c *cacheConfig) {
		if n > 0 {
			c.maxEntries = n
		}
	}
}

// WithSkipCachePaths registers path prefixes that opt out of caching. Any
// request whose URL path starts with one of these prefixes bypasses the cache
// entirely — it is neither read from nor written to the cache.
func WithSkipCachePaths(paths ...string) CacheOption {
	return func(c *cacheConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

// cacheEntry holds a captured HTTP response.
type cacheEntry struct {
	body      []byte
	header    http.Header
	status    int
	createdAt time.Time
}

// lruEntry is the value stored inside the doubly-linked list node.
type lruEntry struct {
	key   string
	value *cacheEntry
}

// lruCache is a thread-safe LRU cache backed by a map and a doubly-linked
// list. The list orders entries from most-recently-used (front) to
// least-recently-used (back). Eviction removes from the back.
type lruCache struct {
	mu      sync.RWMutex
	entries map[string]*list.Element
	order   *list.List
	maxSize int
}

// newLRUCache allocates an lruCache with the given maximum size.
func newLRUCache(maxSize int) *lruCache {
	return &lruCache{
		entries: make(map[string]*list.Element, maxSize),
		order:   list.New(),
		maxSize: maxSize,
	}
}

// get returns the cache entry for key and promotes it to the front of the LRU
// list. The second return value is false when the key is absent.
// Callers must hold at least a read lock; this method upgrades to a write lock
// internally when promotion is required.
func (c *lruCache) get(key string) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	// Promote to front (most-recently used).
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry).value, true
}

// set inserts or replaces the entry for key and promotes it to the front.
// When the cache is at capacity the least-recently-used entry is evicted first.
func (c *lruCache) set(key string, entry *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[key]; ok {
		// Update in place and promote.
		el.Value.(*lruEntry).value = entry
		c.order.MoveToFront(el)
		return
	}

	// Evict the LRU entry when at capacity.
	if c.order.Len() >= c.maxSize {
		c.evictLRULocked()
	}

	el := c.order.PushFront(&lruEntry{key: key, value: entry})
	c.entries[key] = el
}

// evictLRULocked removes the least-recently-used entry from the cache.
// The caller must hold the write lock.
func (c *lruCache) evictLRULocked() {
	back := c.order.Back()
	if back == nil {
		return
	}
	c.order.Remove(back)
	delete(c.entries, back.Value.(*lruEntry).key)
}

// invalidatePrefix removes all entries whose key path component starts with
// the given prefix. This is called on write operations (POST/PATCH/PUT/DELETE)
// to evict stale GET responses for the affected resource collection.
func (c *lruCache) invalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, el := range c.entries {
		// Cache key format: "{method}:{path}:{query}". Extract the path segment.
		path := extractPathFromKey(key)
		if strings.HasPrefix(path, prefix) {
			c.order.Remove(el)
			delete(c.entries, key)
		}
	}
}

// responseRecorder wraps http.ResponseWriter and buffers the response so the
// Cache middleware can inspect the status and body before storing them.
type responseRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	wrote  bool
}

// WriteHeader captures the status code without forwarding it yet.
func (rr *responseRecorder) WriteHeader(status int) {
	if !rr.wrote {
		rr.status = status
		rr.wrote = true
	}
}

// Write accumulates body bytes into the internal buffer.
func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.wrote {
		rr.status = http.StatusOK
		rr.wrote = true
	}
	return rr.body.Write(b)
}

// Flush delegates to the underlying http.Flusher when the response writer
// supports it. Because the body is buffered, this is a best-effort signal.
func (rr *responseRecorder) Flush() {
	if f, ok := rr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Cache returns a middleware that caches GET responses in memory using an LRU
// eviction policy.
//
// Cache key: "{method}:{path}:{sorted_query_string}"
//
// Caching rules:
//   - Only GET requests are eligible for caching.
//   - Only 2xx responses are stored in the cache.
//   - Paths matching any prefix in WithSkipCachePaths bypass the cache.
//   - Cached responses include the original response headers, status, and body.
//
// On a cache hit the middleware short-circuits: it replays the stored response
// and sets "X-Cache: HIT" plus "Cache-Control: max-age=<ttl_seconds>".
//
// On a cache miss the middleware captures the upstream response, stores it (if
// eligible), and sets "X-Cache: MISS" plus "Cache-Control: max-age=<ttl_seconds>".
//
// Expiration is lazy: expired entries are detected at access time and treated
// as a miss. The stale entry is replaced by the fresh response.
//
// Write operations (POST, PATCH, PUT, DELETE) trigger prefix-based
// invalidation: all cached entries whose path starts with the request path are
// evicted, ensuring GET responses for the affected collection become stale.
func Cache(opts ...CacheOption) func(http.Handler) http.Handler {
	cfg := &cacheConfig{
		ttl:        60 * time.Second,
		maxEntries: 1000,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	lru := newLRUCache(cfg.maxEntries)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write methods trigger cache invalidation and bypass caching.
			switch r.Method {
			case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
				lru.invalidatePrefix(r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}

			// Only GET requests are cached.
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			// Skip explicitly excluded path prefixes.
			if hasSkipPrefix(r.URL.Path, cfg.skipPaths) {
				next.ServeHTTP(w, r)
				return
			}

			key := buildCacheKey(r)
			ttlSeconds := int(cfg.ttl.Seconds())

			// Cache hit: serve the stored response.
			if entry, ok := lru.get(key); ok && !isExpired(entry, cfg.ttl) {
				replayEntry(w, entry, ttlSeconds)
				return
			}

			// Cache miss: capture the upstream response.
			rec := &responseRecorder{
				ResponseWriter: w,
				status:         http.StatusOK,
			}
			next.ServeHTTP(rec, r)

			// Copy captured headers to the real writer before deciding on
			// Cache-Control and X-Cache so they are not overwritten below.
			for k, vals := range rec.Header() {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}

			body := rec.body.Bytes()
			status := rec.status

			// Only store successful responses.
			if status >= http.StatusOK && status < http.StatusMultipleChoices {
				// Clone the header map so the cache entry is not mutated later
				// by the ResponseWriter's header manipulation.
				headerSnapshot := cloneHeader(rec.Header())
				lru.set(key, &cacheEntry{
					body:      append([]byte(nil), body...),
					header:    headerSnapshot,
					status:    status,
					createdAt: time.Now(),
				})
				w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", ttlSeconds))
				w.Header().Set("X-Cache", "MISS")
			}

			w.WriteHeader(status)
			if len(body) > 0 {
				_, _ = w.Write(body)
			}
		})
	}
}

// replayEntry writes a stored cache entry to w and sets cache-related headers.
func replayEntry(w http.ResponseWriter, entry *cacheEntry, ttlSeconds int) {
	for k, vals := range entry.header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", ttlSeconds))
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(entry.status)
	if len(entry.body) > 0 {
		_, _ = w.Write(entry.body)
	}
}

// buildCacheKey constructs a deterministic cache key for a request.
// Format: "{method}:{path}:{sorted_query_string}"
func buildCacheKey(r *http.Request) string {
	query := sortedQuery(r.URL.Query())
	return fmt.Sprintf("%s:%s:%s", r.Method, r.URL.Path, query)
}

// sortedQuery encodes url.Values with keys in sorted order so that
// ?b=2&a=1 and ?a=1&b=2 produce the same cache key.
func sortedQuery(v url.Values) string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		vals := v[k]
		sort.Strings(vals)
		for j, val := range vals {
			if i > 0 || j > 0 {
				sb.WriteByte('&')
			}
			sb.WriteString(url.QueryEscape(k))
			sb.WriteByte('=')
			sb.WriteString(url.QueryEscape(val))
		}
	}
	return sb.String()
}

// extractPathFromKey returns the path segment from a cache key of the form
// "{method}:{path}:{query}".
func extractPathFromKey(key string) string {
	// Find first colon (end of method).
	first := strings.IndexByte(key, ':')
	if first < 0 {
		return key
	}
	rest := key[first+1:]
	// Find second colon (end of path).
	second := strings.IndexByte(rest, ':')
	if second < 0 {
		return rest
	}
	return rest[:second]
}

// hasSkipPrefix reports whether path starts with any of the given prefixes.
func hasSkipPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// isExpired reports whether a cache entry has lived longer than ttl.
func isExpired(entry *cacheEntry, ttl time.Duration) bool {
	return time.Since(entry.createdAt) > ttl
}

// cloneHeader returns a deep copy of an http.Header map.
func cloneHeader(h http.Header) http.Header {
	clone := make(http.Header, len(h))
	for k, vals := range h {
		cp := make([]string, len(vals))
		copy(cp, vals)
		clone[k] = cp
	}
	return clone
}
