package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Wire types – JSON representation of a snapshot
// ---------------------------------------------------------------------------

// jsonHistogram is the JSON-serialisable form of a HistogramSnapshot.
type jsonHistogram struct {
	Count int64   `json:"count"`
	SumMs float64 `json:"sum_ms"`
	MinMs float64 `json:"min_ms"`
	MaxMs float64 `json:"max_ms"`
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
}

// jsonSnapshot is the JSON-serialisable form of a MetricsSnapshot.
type jsonSnapshot struct {
	Timestamp  string                   `json:"timestamp"`
	Counters   map[string]int64         `json:"counters"`
	Gauges     map[string]int64         `json:"gauges"`
	Histograms map[string]jsonHistogram `json:"histograms"`
}

func toJSON(snap MetricsSnapshot) jsonSnapshot {
	histos := make(map[string]jsonHistogram, len(snap.Histograms))
	for name, h := range snap.Histograms {
		histos[name] = jsonHistogram{
			Count: h.Count,
			SumMs: msf(h.Sum),
			MinMs: msf(h.Min),
			MaxMs: msf(h.Max),
			P50Ms: msf(h.P50),
			P95Ms: msf(h.P95),
			P99Ms: msf(h.P99),
		}
	}
	return jsonSnapshot{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Counters:   snap.Counters,
		Gauges:     snap.Gauges,
		Histograms: histos,
	}
}

// msf converts a Duration to a float64 expressed in milliseconds.
func msf(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// ---------------------------------------------------------------------------
// Exporter
// ---------------------------------------------------------------------------

// Exporter writes metrics snapshots from a Provider to various destinations.
// Use NewExporter to construct one; the zero value is not usable.
type Exporter struct {
	provider *Provider
}

// NewExporter creates an Exporter that reads from p.
func NewExporter(p *Provider) *Exporter {
	return &Exporter{provider: p}
}

// WriteJSON encodes the current snapshot as a single JSON object and writes it
// to w. The output is a single line terminated by a newline character.
func (e *Exporter) WriteJSON(w io.Writer) error {
	snap := e.provider.Snapshot()
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toJSON(snap))
}

// Handler returns an http.Handler that serves the current metrics snapshot as
// JSON at any path. The response Content-Type is application/json and the
// status is always 200 OK unless the snapshot encoding fails (500).
//
// Typical usage:
//
//	mux.Handle("/metrics", exporter.Handler())
func (e *Exporter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := e.WriteJSON(w); err != nil {
			http.Error(w, "failed to encode metrics: "+err.Error(), http.StatusInternalServerError)
		}
	})
}
