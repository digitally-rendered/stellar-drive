// Package telemetry provides a lightweight, stdlib-only observability layer for
// stellar-drive. It collects counters, gauges, and duration histograms in-process
// and exposes them via an HTTP metrics handler.
//
// Design constraints:
//   - No external dependencies; stdlib only.
//   - Safe for concurrent use from many goroutines.
//   - Histograms use reservoir sampling (Vitter's Algorithm R) so memory is
//     bounded regardless of observation count.
package telemetry

import (
	"math/rand/v2"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// reservoirSize is the maximum number of duration samples kept per histogram.
// 1 024 samples give sub-percent accuracy for the common percentiles.
const reservoirSize = 1024

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// Histogram tracks a bounded sample of duration observations and derives
// summary statistics from that sample. It is safe for concurrent use.
type Histogram struct {
	mu       sync.Mutex
	count    int64   // total observations ever recorded (unbounded)
	sum      int64   // sum of all observations in nanoseconds
	min      int64   // minimum observation in nanoseconds
	max      int64   // maximum observation in nanoseconds
	sample   []int64 // reservoir; length <= reservoirSize
	hasValue bool    // true once the first observation has been recorded
}

// record adds a single observation (in nanoseconds) to the histogram.
// It uses Vitter's Algorithm R for reservoir sampling so that every
// observation has an equal probability of appearing in the sample.
func (h *Histogram) record(ns int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += ns

	if !h.hasValue {
		h.min = ns
		h.max = ns
		h.hasValue = true
	} else {
		if ns < h.min {
			h.min = ns
		}
		if ns > h.max {
			h.max = ns
		}
	}

	if int64(len(h.sample)) < reservoirSize {
		h.sample = append(h.sample, ns)
	} else {
		// Replace a random element with probability reservoirSize/count.
		j := rand.Int64N(h.count)
		if j < reservoirSize {
			h.sample[j] = ns
		}
	}
}

// snapshot returns an immutable copy of the histogram's current state.
func (h *Histogram) snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap := HistogramSnapshot{
		Count: h.count,
		Sum:   time.Duration(h.sum),
	}
	if h.hasValue {
		snap.Min = time.Duration(h.min)
		snap.Max = time.Duration(h.max)
	}
	if len(h.sample) > 0 {
		sorted := make([]int64, len(h.sample))
		copy(sorted, h.sample)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		snap.P50 = time.Duration(percentile(sorted, 0.50))
		snap.P95 = time.Duration(percentile(sorted, 0.95))
		snap.P99 = time.Duration(percentile(sorted, 0.99))
	}
	return snap
}

// percentile returns the value at rank p (0.0–1.0) of the already-sorted
// slice s using nearest-rank interpolation.
func percentile(s []int64, p float64) int64 {
	if len(s) == 0 {
		return 0
	}
	idx := int(p * float64(len(s)-1))
	return s[idx]
}

// ---------------------------------------------------------------------------
// Snapshots (value types for safe reads without holding locks)
// ---------------------------------------------------------------------------

// HistogramSnapshot is an immutable point-in-time view of a Histogram.
type HistogramSnapshot struct {
	Count int64
	Sum   time.Duration
	Min   time.Duration
	Max   time.Duration
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
}

// MetricsSnapshot is an immutable point-in-time view of all metrics tracked
// by a Provider.
type MetricsSnapshot struct {
	Counters   map[string]int64
	Gauges     map[string]int64
	Histograms map[string]HistogramSnapshot
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// Provider collects request metrics and exposes them. All methods are safe for
// concurrent use from multiple goroutines.
type Provider struct {
	mu       sync.RWMutex
	counters map[string]*int64
	gauges   map[string]*int64
	histos   map[string]*Histogram
}

// NewProvider creates a ready-to-use Provider.
func NewProvider() *Provider {
	return &Provider{
		counters: make(map[string]*int64),
		gauges:   make(map[string]*int64),
		histos:   make(map[string]*Histogram),
	}
}

// IncrCounter atomically increments the named counter by delta. If the counter
// does not yet exist it is created with an initial value of delta.
func (p *Provider) IncrCounter(name string, delta int64) {
	ptr := p.counterPtr(name)
	atomic.AddInt64(ptr, delta)
}

// SetGauge sets the named gauge to value, overwriting any previous value.
func (p *Provider) SetGauge(name string, value int64) {
	ptr := p.gaugePtr(name)
	atomic.StoreInt64(ptr, value)
}

// RecordDuration records a duration observation in the named histogram.
func (p *Provider) RecordDuration(name string, d time.Duration) {
	h := p.histoFor(name)
	h.record(d.Nanoseconds())
}

// Snapshot returns a deep copy of all current metrics so callers can read
// values without holding any lock.
func (p *Provider) Snapshot() MetricsSnapshot {
	p.mu.RLock()
	counters := make(map[string]int64, len(p.counters))
	for k, v := range p.counters {
		counters[k] = atomic.LoadInt64(v)
	}
	gauges := make(map[string]int64, len(p.gauges))
	for k, v := range p.gauges {
		gauges[k] = atomic.LoadInt64(v)
	}
	histoNames := make([]string, 0, len(p.histos))
	for k := range p.histos {
		histoNames = append(histoNames, k)
	}
	histoRefs := make(map[string]*Histogram, len(p.histos))
	for k, v := range p.histos {
		histoRefs[k] = v
	}
	p.mu.RUnlock()

	histos := make(map[string]HistogramSnapshot, len(histoNames))
	for _, name := range histoNames {
		histos[name] = histoRefs[name].snapshot()
	}

	return MetricsSnapshot{
		Counters:   counters,
		Gauges:     gauges,
		Histograms: histos,
	}
}

// Middleware returns an HTTP middleware that records three metrics per route
// pattern (the value of http.Request.Pattern, populated by net/http's 1.22+
// router, or the raw URL path when no pattern is matched):
//
//   - http_requests_total{pattern}         – counter, incremented per request.
//   - http_request_duration_ns{pattern}    – histogram of response times.
//   - http_active_requests{pattern}        – gauge of in-flight requests.
func (p *Provider) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pattern := r.Pattern
			if pattern == "" {
				pattern = r.URL.Path
			}

			totalKey := "http_requests_total{" + pattern + "}"
			durationKey := "http_request_duration_ns{" + pattern + "}"
			activeKey := "http_active_requests{" + pattern + "}"

			p.IncrCounter(totalKey, 1)
			p.SetGauge(activeKey, atomic.AddInt64(p.gaugePtr(activeKey), 1))

			start := time.Now()
			defer func() {
				p.RecordDuration(durationKey, time.Since(start))
				atomic.AddInt64(p.gaugePtr(activeKey), -1)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Internal helpers – lazy initialisation under lock
// ---------------------------------------------------------------------------

func (p *Provider) counterPtr(name string) *int64 {
	p.mu.RLock()
	ptr, ok := p.counters[name]
	p.mu.RUnlock()
	if ok {
		return ptr
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if ptr, ok = p.counters[name]; ok {
		return ptr
	}
	v := new(int64)
	p.counters[name] = v
	return v
}

func (p *Provider) gaugePtr(name string) *int64 {
	p.mu.RLock()
	ptr, ok := p.gauges[name]
	p.mu.RUnlock()
	if ok {
		return ptr
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if ptr, ok = p.gauges[name]; ok {
		return ptr
	}
	v := new(int64)
	p.gauges[name] = v
	return v
}

func (p *Provider) histoFor(name string) *Histogram {
	p.mu.RLock()
	h, ok := p.histos[name]
	p.mu.RUnlock()
	if ok {
		return h
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok = p.histos[name]; ok {
		return h
	}
	h = &Histogram{}
	p.histos[name] = h
	return h
}
