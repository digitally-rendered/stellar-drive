package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// bucket is a token-bucket limiter for a single client.
type bucket struct {
	mu             sync.Mutex
	tokens         float64
	maxTokens      float64
	refillRate     float64 // tokens per nanosecond
	lastRefillTime time.Time
}

// newBucket returns a full bucket with the given capacity and refill rate.
func newBucket(requestsPerSecond int, burst int) *bucket {
	b := &bucket{
		tokens:         float64(burst),
		maxTokens:      float64(burst),
		refillRate:     float64(requestsPerSecond) / float64(time.Second),
		lastRefillTime: time.Now(),
	}
	return b
}

// allow attempts to consume one token. It returns true if a token was
// available and false (with the number of seconds to wait) if the bucket is
// empty.
func (b *bucket) allow() (ok bool, retryAfter int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefillTime)
	b.lastRefillTime = now

	// Refill tokens proportional to elapsed time.
	b.tokens += float64(elapsed) * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Calculate how long until the next token is available.
	deficit := 1 - b.tokens
	waitNs := deficit / b.refillRate
	waitSec := int(waitNs/float64(time.Second)) + 1
	return false, waitSec
}

// rateLimiter holds per-IP buckets with periodic cleanup of idle entries.
type rateLimiter struct {
	mu                sync.Mutex
	buckets           map[string]*bucket
	requestsPerSecond int
	burst             int
}

// newRateLimiter constructs a rateLimiter.
func newRateLimiter(requestsPerSecond, burst int) *rateLimiter {
	rl := &rateLimiter{
		buckets:           make(map[string]*bucket),
		requestsPerSecond: requestsPerSecond,
		burst:             burst,
	}
	// Periodically evict idle buckets to prevent unbounded memory growth.
	go rl.cleanup()
	return rl
}

// cleanup removes buckets that have been idle for more than 5 minutes.
func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			b.mu.Lock()
			idle := now.Sub(b.lastRefillTime)
			b.mu.Unlock()
			if idle > 5*time.Minute {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// allow returns whether the request from ip should be allowed, and if not, the
// number of seconds the caller should wait before retrying.
func (rl *rateLimiter) allow(ip string) (bool, int) {
	rl.mu.Lock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = newBucket(rl.requestsPerSecond, rl.burst)
		rl.buckets[ip] = b
	}
	rl.mu.Unlock()
	return b.allow()
}

// RateLimit returns an HTTP middleware that enforces a per-client-IP token
// bucket rate limit.
//
// requestsPerSecond controls the steady-state refill rate. burst sets the
// maximum number of requests that can be made in a single instant (the initial
// and maximum token count). When a client exhausts its tokens the middleware
// responds with 429 Too Many Requests and sets a Retry-After header indicating
// how many seconds to wait.
func RateLimit(requestsPerSecond int, burst int) func(http.Handler) http.Handler {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 10
	}
	if burst <= 0 {
		burst = requestsPerSecond
	}
	rl := newRateLimiter(requestsPerSecond, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			ok, retryAfter := rl.allow(ip)
			if !ok {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":"rate limit exceeded","retry_after":%d}`, retryAfter) //nolint:errcheck
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the client IP address from the request. It checks
// X-Forwarded-For and X-Real-IP before falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may be a comma-separated list; take the first entry.
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return xrip
	}
	// RemoteAddr is "IP:port"; strip the port.
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		addr = addr[:i]
	}
	return addr
}
