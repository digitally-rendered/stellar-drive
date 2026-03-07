package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// IncrCounter
// ---------------------------------------------------------------------------

func TestProvider_IncrCounter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ops  []int64 // sequence of delta values to apply
		want int64
	}{
		{
			name: "single increment",
			ops:  []int64{1},
			want: 1,
		},
		{
			name: "multiple increments accumulate",
			ops:  []int64{1, 2, 3},
			want: 6,
		},
		{
			name: "decrement reduces counter",
			ops:  []int64{10, -3},
			want: 7,
		},
		{
			name: "large delta",
			ops:  []int64{1000000},
			want: 1000000,
		},
		{
			name: "zero delta leaves counter unchanged",
			ops:  []int64{5, 0},
			want: 5,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := NewProvider()
			for _, d := range tc.ops {
				p.IncrCounter("requests", d)
			}

			snap := p.Snapshot()
			assert.Equal(t, tc.want, snap.Counters["requests"])
		})
	}
}

func TestProvider_IncrCounter_Concurrent(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	const goroutines = 100
	const incrementsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				p.IncrCounter("concurrent_counter", 1)
			}
		}()
	}
	wg.Wait()

	snap := p.Snapshot()
	assert.Equal(t, int64(goroutines*incrementsPerGoroutine), snap.Counters["concurrent_counter"])
}

// ---------------------------------------------------------------------------
// SetGauge
// ---------------------------------------------------------------------------

func TestProvider_SetGauge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ops  []int64 // sequence of values to set
		want int64
	}{
		{
			name: "set once",
			ops:  []int64{42},
			want: 42,
		},
		{
			name: "later set overwrites earlier",
			ops:  []int64{10, 20, 30},
			want: 30,
		},
		{
			name: "set to zero",
			ops:  []int64{99, 0},
			want: 0,
		},
		{
			name: "set negative value",
			ops:  []int64{-7},
			want: -7,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := NewProvider()
			for _, v := range tc.ops {
				p.SetGauge("active_connections", v)
			}

			snap := p.Snapshot()
			assert.Equal(t, tc.want, snap.Gauges["active_connections"])
		})
	}
}

// ---------------------------------------------------------------------------
// RecordDuration / Histogram
// ---------------------------------------------------------------------------

func TestProvider_RecordDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		observations   []time.Duration
		wantCount      int64
		wantMinAtMost  time.Duration // min <= this
		wantMaxAtLeast time.Duration // max >= this
		wantP50Pos     bool          // P50 > 0
	}{
		{
			name:           "single observation",
			observations:   []time.Duration{10 * time.Millisecond},
			wantCount:      1,
			wantMinAtMost:  10 * time.Millisecond,
			wantMaxAtLeast: 10 * time.Millisecond,
		},
		{
			name: "multiple observations set min and max",
			observations: []time.Duration{
				1 * time.Millisecond,
				50 * time.Millisecond,
				100 * time.Millisecond,
			},
			wantCount:      3,
			wantMinAtMost:  1 * time.Millisecond,
			wantMaxAtLeast: 100 * time.Millisecond,
			wantP50Pos:     true,
		},
		{
			name: "many observations populate percentiles",
			observations: func() []time.Duration {
				ds := make([]time.Duration, 200)
				for i := range ds {
					ds[i] = time.Duration(i+1) * time.Millisecond
				}
				return ds
			}(),
			wantCount:      200,
			wantMinAtMost:  time.Millisecond,
			wantMaxAtLeast: 200 * time.Millisecond,
			wantP50Pos:     true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := NewProvider()
			for _, d := range tc.observations {
				p.RecordDuration("request_latency", d)
			}

			snap := p.Snapshot()
			h, ok := snap.Histograms["request_latency"]
			require.True(t, ok, "histogram should exist after recording")

			assert.Equal(t, tc.wantCount, h.Count)
			assert.LessOrEqual(t, h.Min, tc.wantMinAtMost)
			assert.GreaterOrEqual(t, h.Max, tc.wantMaxAtLeast)
			assert.Greater(t, h.Sum, time.Duration(0))
			if tc.wantP50Pos {
				assert.Greater(t, h.P50, time.Duration(0))
			}
		})
	}
}

func TestHistogram_PercentilesOrdering(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	// Record 500 observations uniformly distributed from 1ms to 500ms.
	for i := 1; i <= 500; i++ {
		p.RecordDuration("latency", time.Duration(i)*time.Millisecond)
	}

	snap := p.Snapshot()
	h := snap.Histograms["latency"]

	assert.Equal(t, int64(500), h.Count)
	// Percentile ordering must hold: P50 <= P95 <= P99 <= Max.
	assert.LessOrEqual(t, h.P50, h.P95, "P50 must be <= P95")
	assert.LessOrEqual(t, h.P95, h.P99, "P95 must be <= P99")
	assert.LessOrEqual(t, h.P99, h.Max, "P99 must be <= Max")
}

// ---------------------------------------------------------------------------
// Snapshot isolation
// ---------------------------------------------------------------------------

func TestProvider_Snapshot_IsolatesMutation(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	p.IncrCounter("hits", 5)
	p.SetGauge("queue_depth", 10)

	snap1 := p.Snapshot()

	// Mutate after taking the first snapshot.
	p.IncrCounter("hits", 3)
	p.SetGauge("queue_depth", 99)

	snap2 := p.Snapshot()

	// snap1 must not reflect the later mutations.
	assert.Equal(t, int64(5), snap1.Counters["hits"])
	assert.Equal(t, int64(10), snap1.Gauges["queue_depth"])

	// snap2 must reflect all mutations.
	assert.Equal(t, int64(8), snap2.Counters["hits"])
	assert.Equal(t, int64(99), snap2.Gauges["queue_depth"])
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func TestProvider_Middleware_RecordsCounter(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	mw := p.Middleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const requests = 5
	for i := 0; i < requests; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		handler.ServeHTTP(rec, req)
	}

	snap := p.Snapshot()

	// The counter key uses URL path as pattern because httptest.NewRequest
	// does not go through the mux, so r.Pattern is empty.
	var totalReqs int64
	for k, v := range snap.Counters {
		_ = k
		totalReqs += v
	}
	assert.Equal(t, int64(requests), totalReqs,
		"total request counter should equal number of requests made")
}

func TestProvider_Middleware_RecordsDuration(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	mw := p.Middleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	handler.ServeHTTP(rec, req)

	snap := p.Snapshot()

	var found bool
	for _, h := range snap.Histograms {
		found = true
		assert.Equal(t, int64(1), h.Count)
		assert.Greater(t, h.Max, time.Duration(0))
	}
	assert.True(t, found, "at least one histogram should be recorded")
}

func TestProvider_Middleware_TracksActiveRequests(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	mw := p.Middleware()

	// Use a channel to hold the handler mid-flight so we can inspect the gauge.
	unblock := make(chan struct{})
	active := make(chan int64, 1)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the active gauge while the request is still in-flight.
		snap := p.Snapshot()
		for k, v := range snap.Gauges {
			_ = k
			if v > 0 {
				active <- v
				break
			}
		}
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))

	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/stream", nil)
		handler.ServeHTTP(rec, req)
	}()

	// Wait until the in-flight gauge value has been published.
	var gaugeWhileActive int64
	select {
	case gaugeWhileActive = <-active:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight gauge")
	}
	close(unblock)

	assert.Equal(t, int64(1), gaugeWhileActive,
		"active request gauge should be 1 while a request is in flight")

	// After the request completes the gauge must return to 0.
	// Give the defer a moment to execute.
	time.Sleep(10 * time.Millisecond)
	snap := p.Snapshot()
	for k, v := range snap.Gauges {
		if k != "" {
			assert.Equal(t, int64(0), v,
				"active request gauge for %q should be 0 after request completes", k)
		}
	}
}

// ---------------------------------------------------------------------------
// Multiple independent metrics
// ---------------------------------------------------------------------------

func TestProvider_MultipleMetricNames(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	p.IncrCounter("alpha", 1)
	p.IncrCounter("beta", 2)
	p.IncrCounter("alpha", 3)

	snap := p.Snapshot()
	assert.Equal(t, int64(4), snap.Counters["alpha"])
	assert.Equal(t, int64(2), snap.Counters["beta"])
	_, betaExists := snap.Counters["beta"]
	assert.True(t, betaExists)
}

// ---------------------------------------------------------------------------
// Exporter
// ---------------------------------------------------------------------------

func TestExporter_Handler_ReturnsJSON(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	p.IncrCounter("requests", 7)
	p.SetGauge("workers", 3)
	p.RecordDuration("latency", 50*time.Millisecond)

	exp := NewExporter(p)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	exp.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))

	var body jsonSnapshot
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err, "response body must be valid JSON")

	assert.Equal(t, int64(7), body.Counters["requests"])
	assert.Equal(t, int64(3), body.Gauges["workers"])

	latency, ok := body.Histograms["latency"]
	require.True(t, ok, "latency histogram must appear in JSON output")
	assert.Equal(t, int64(1), latency.Count)
	assert.Greater(t, latency.MaxMs, 0.0)
}

func TestExporter_WriteJSON_IncludesTimestamp(t *testing.T) {
	t.Parallel()

	p := NewProvider()
	exp := NewExporter(p)

	var buf strings.Builder
	err := exp.WriteJSON(&buf)
	require.NoError(t, err)

	var body jsonSnapshot
	err = json.Unmarshal([]byte(buf.String()), &body)
	require.NoError(t, err)

	_, parseErr := time.Parse(time.RFC3339, body.Timestamp)
	assert.NoError(t, parseErr, "timestamp must be valid RFC3339")
}
