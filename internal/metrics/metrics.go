// Package metrics exposes lightweight, dependency-free counters and histograms.
// Everything is safe for concurrent use and rendered in Prometheus text format.
package metrics

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing value.
type Counter struct {
	v atomic.Int64
}

func (c *Counter) Inc()         { c.v.Add(1) }
func (c *Counter) Add(n int64)  { c.v.Add(n) }
func (c *Counter) Value() int64 { return c.v.Load() }

// Gauge is a value that can go up and down.
type Gauge struct {
	v atomic.Int64
}

func (g *Gauge) Inc()         { g.v.Add(1) }
func (g *Gauge) Dec()         { g.v.Add(-1) }
func (g *Gauge) Set(n int64)  { g.v.Store(n) }
func (g *Gauge) Value() int64 { return g.v.Load() }

// Histogram bucketed in milliseconds, used for queue waits and signaling latency.
type Histogram struct {
	mu     sync.Mutex
	bounds []float64
	counts []int64
	sumMs  float64
	count  int64
}

func newHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]int64, len(bounds)+1)}
}

// Observe records a duration in milliseconds.
func (h *Histogram) Observe(d time.Duration) {
	ms := float64(d.Microseconds()) / 1000.0
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sumMs += ms
	h.count++
	for i, b := range h.bounds {
		if ms <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.bounds)]++
}

// Quantile returns the cumulative-bucket estimate for q (0..1) in milliseconds.
func (h *Histogram) Quantile(q float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	target := int64(float64(h.count) * q)
	var cumulative int64
	for i, c := range h.counts {
		cumulative += c
		if cumulative >= target {
			if i < len(h.bounds) {
				return h.bounds[i]
			}
			return h.bounds[len(h.bounds)-1]
		}
	}
	return h.bounds[len(h.bounds)-1]
}

// Mean returns the arithmetic mean in milliseconds, or 0 when unobserved.
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sumMs / float64(h.count)
}

// Count returns how many observations were recorded.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Metrics aggregates everything the operating team needs to watch.
type Metrics struct {
	StartedAt time.Time

	ConnectionsOpened Counter
	ConnectionsClosed Counter
	ConnectionsActive Gauge
	ConnectionsKicked Counter

	QueueJoins    Counter
	QueueCancels  Counter
	QueueTimeouts Counter
	Matches       Counter

	MessagesIn       Counter
	MessagesOut      Counter
	MessagesRejected Counter
	BytesIn          Counter
	BytesOut         Counter

	Reports     Counter
	Blocks      Counter
	Suspensions Counter
	Violations  Counter

	MatchWait       *Histogram
	SignalLatency   *Histogram
	SessionDuration *Histogram
}

// New builds a Metrics registry with sensible latency buckets.
func New() *Metrics {
	return &Metrics{
		StartedAt:       time.Now().UTC(),
		MatchWait:       newHistogram([]float64{50, 100, 250, 500, 1000, 2500, 5000, 15000, 30000, 60000}),
		SignalLatency:   newHistogram([]float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500}),
		SessionDuration: newHistogram([]float64{5000, 15000, 30000, 60000, 120000, 300000, 600000}),
	}
}

// Uptime returns how long the process has been serving.
func (m *Metrics) Uptime() time.Duration { return time.Since(m.StartedAt) }

// Snapshot returns a JSON-friendly view used by /api/stats.
func (m *Metrics) Snapshot() map[string]any {
	return map[string]any{
		"uptime_seconds":        int64(m.Uptime().Seconds()),
		"connections_active":    m.ConnectionsActive.Value(),
		"connections_opened":    m.ConnectionsOpened.Value(),
		"connections_closed":    m.ConnectionsClosed.Value(),
		"queue_joins":           m.QueueJoins.Value(),
		"queue_cancels":         m.QueueCancels.Value(),
		"queue_timeouts":        m.QueueTimeouts.Value(),
		"matches":               m.Matches.Value(),
		"messages_in":           m.MessagesIn.Value(),
		"messages_out":          m.MessagesOut.Value(),
		"messages_rejected":     m.MessagesRejected.Value(),
		"reports":               m.Reports.Value(),
		"blocks":                m.Blocks.Value(),
		"suspensions":           m.Suspensions.Value(),
		"match_wait_avg_ms":     round(m.MatchWait.Mean()),
		"match_wait_p95_ms":     round(m.MatchWait.Quantile(0.95)),
		"signal_latency_p95_ms": round(m.SignalLatency.Quantile(0.95)),
	}
}

// WritePrometheus renders the registry in the Prometheus text exposition format.
func (m *Metrics) WritePrometheus(w io.Writer) {
	type entry struct {
		name string
		help string
		kind string
		val  int64
	}
	entries := []entry{
		{"randomchat_connections_active", "WebSocket connections currently open", "gauge", m.ConnectionsActive.Value()},
		{"randomchat_connections_opened_total", "WebSocket connections accepted since start", "counter", m.ConnectionsOpened.Value()},
		{"randomchat_connections_closed_total", "WebSocket connections closed since start", "counter", m.ConnectionsClosed.Value()},
		{"randomchat_connections_kicked_total", "Connections closed because the client was too slow", "counter", m.ConnectionsKicked.Value()},
		{"randomchat_queue_joins_total", "Queue join requests accepted", "counter", m.QueueJoins.Value()},
		{"randomchat_queue_cancels_total", "Queue searches cancelled by the user", "counter", m.QueueCancels.Value()},
		{"randomchat_queue_timeouts_total", "Searches abandoned after the queue timeout", "counter", m.QueueTimeouts.Value()},
		{"randomchat_matches_total", "Sessions successfully created", "counter", m.Matches.Value()},
		{"randomchat_messages_in_total", "Client messages accepted", "counter", m.MessagesIn.Value()},
		{"randomchat_messages_out_total", "Server messages written", "counter", m.MessagesOut.Value()},
		{"randomchat_messages_rejected_total", "Client messages rejected by validation", "counter", m.MessagesRejected.Value()},
		{"randomchat_bytes_in_total", "Bytes received on WebSockets", "counter", m.BytesIn.Value()},
		{"randomchat_bytes_out_total", "Bytes written to WebSockets", "counter", m.BytesOut.Value()},
		{"randomchat_reports_total", "Reports filed by users", "counter", m.Reports.Value()},
		{"randomchat_blocks_total", "Blocks created by users", "counter", m.Blocks.Value()},
		{"randomchat_suspensions_total", "Automatic suspensions issued", "counter", m.Suspensions.Value()},
		{"randomchat_violations_total", "Rate-limit violations observed", "counter", m.Violations.Value()},
		{"randomchat_uptime_seconds", "Process uptime", "gauge", int64(m.Uptime().Seconds())},
	}
	for _, e := range entries {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", e.name, e.help, e.name, e.kind, e.name, e.val)
	}
	for _, h := range []struct {
		name string
		help string
		hist *Histogram
	}{
		{"randomchat_match_wait_ms", "Time between queue join and match, milliseconds", m.MatchWait},
		{"randomchat_signal_latency_ms", "Time to route a signaling message, milliseconds", m.SignalLatency},
	} {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
		h.hist.mu.Lock()
		var cumulative int64
		for i, b := range h.hist.bounds {
			cumulative += h.hist.counts[i]
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", h.name, b, cumulative)
		}
		cumulative += h.hist.counts[len(h.hist.bounds)]
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, cumulative)
		fmt.Fprintf(w, "%s_sum %g\n%s_count %d\n", h.name, h.hist.sumMs, h.name, h.hist.count)
		h.hist.mu.Unlock()
	}
}

func round(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
