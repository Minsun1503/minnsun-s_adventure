package metrics

import (
	"fmt"
	"net/http"
	"server/peakgo/perf"
)

// MetricsServer exposes peakgo/perf metrics via Prometheus-compatible /metrics endpoint.
type MetricsServer struct {
	tm     *perf.TickMonitor
	pm     *perf.PacketMonitor
	mm     *perf.MemMonitor
	mux    *http.ServeMux
	server *http.Server
}

// NewMetricsServer creates a new metrics HTTP server.
func NewMetricsServer(addr string, tm *perf.TickMonitor, pm *perf.PacketMonitor, mm *perf.MemMonitor) *MetricsServer {
	ms := &MetricsServer{
		tm:  tm,
		pm:  pm,
		mm:  mm,
		mux: http.NewServeMux(),
	}
	ms.mux.HandleFunc("/metrics", ms.handleMetrics)
	ms.server = &http.Server{
		Addr:    addr,
		Handler: ms.mux,
	}
	return ms
}

// Start begins serving the /metrics endpoint in a goroutine.
func (ms *MetricsServer) Start() {
	go func() {
		if err := ms.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[metrics] server error: %v\n", err)
		}
	}()
}

// Stop gracefully shuts down the metrics server.
func (ms *MetricsServer) Stop() error {
	return ms.server.Close()
}

// Handler returns the HTTP handler for direct mounting into an existing server.
func (ms *MetricsServer) Handler() http.Handler {
	return ms.mux
}

func (ms *MetricsServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	report := perf.Collect(ms.tm, ms.pm, ms.mm)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Write metrics in Prometheus exposition format
	fmt.Fprintf(w, "# HELP peakgo_tick_duration_ns Game loop tick duration in nanoseconds\n")
	fmt.Fprintf(w, "# TYPE peakgo_tick_duration_ns gauge\n")
	fmt.Fprintf(w, "peakgo_tick_min_ns %d\n", report.TickMin.Nanoseconds())
	fmt.Fprintf(w, "peakgo_tick_max_ns %d\n", report.TickMax.Nanoseconds())
	fmt.Fprintf(w, "peakgo_tick_avg_ns %d\n", report.TickAvg.Nanoseconds())
	fmt.Fprintf(w, "peakgo_tick_total %d\n", report.TickCount)

	fmt.Fprintf(w, "\n# HELP peakgo_packets_total Total packet count\n")
	fmt.Fprintf(w, "# TYPE peakgo_packets_total counter\n")
	fmt.Fprintf(w, "peakgo_packets_in_total %d\n", report.PacketsIn)
	fmt.Fprintf(w, "peakgo_packets_out_total %d\n", report.PacketsOut)

	fmt.Fprintf(w, "\n# HELP peakgo_bytes_total Total byte count\n")
	fmt.Fprintf(w, "# TYPE peakgo_bytes_total counter\n")
	fmt.Fprintf(w, "peakgo_bytes_in_total %d\n", report.BytesIn)
	fmt.Fprintf(w, "peakgo_bytes_out_total %d\n", report.BytesOut)

	fmt.Fprintf(w, "\n# HELP peakgo_memory_bytes Memory allocation\n")
	fmt.Fprintf(w, "# TYPE peakgo_memory_bytes gauge\n")
	fmt.Fprintf(w, "peakgo_alloc_bytes %d\n", report.Alloc)
	fmt.Fprintf(w, "peakgo_heap_objects %d\n", report.HeapObjects)
	fmt.Fprintf(w, "peakgo_goroutines %d\n", report.Goroutines)
	fmt.Fprintf(w, "peakgo_gc_total %d\n", report.NumGC)
}

// Address returns the listen address.
func (ms *MetricsServer) Address() string {
	return ms.server.Addr
}
