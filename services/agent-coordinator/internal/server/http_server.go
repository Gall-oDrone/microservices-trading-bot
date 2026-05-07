package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"bitso-trading-platform/agent-coordinator/internal/coordinator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	httpServer *http.Server
	coord      *coordinator.Coordinator

	mu      sync.RWMutex
	reports map[string]coordinator.CoordinatedReport

	runsTotal   prometheus.Counter
	runsFailed  prometheus.Counter
	runDuration prometheus.Histogram
}

func New(addr string, coord *coordinator.Coordinator) *Server {
	runsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "coordinator_runs_total",
		Help: "Total number of coordinator runs",
	})
	runsFailed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "coordinator_failures_total",
		Help: "Total number of failed coordinator runs",
	})
	runDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "coordinator_run_latency_ms",
		Help:    "Coordinator run latency in milliseconds",
		Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000},
	})
	prometheus.MustRegister(runsTotal, runsFailed, runDuration)

	s := &Server{
		coord:       coord,
		reports:     map[string]coordinator.CoordinatedReport{},
		runsTotal:   runsTotal,
		runsFailed:  runsFailed,
		runDuration: runDuration,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/v1/coordinator/run", s.handleRun)
	mux.HandleFunc("/api/v1/coordinator/report", s.handleGetReport)
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","service":"agent-coordinator"}`))
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var inc coordinator.Incident
	if err := json.NewDecoder(r.Body).Decode(&inc); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	runID := fmt.Sprintf("coord-%d", time.Now().UTC().UnixNano())
	start := time.Now()
	s.runsTotal.Inc()
	report, err := s.coord.HandleIncident(r.Context(), runID, inc)
	s.runDuration.Observe(float64(time.Since(start).Milliseconds()))
	if err != nil {
		s.runsFailed.Inc()
		http.Error(w, "coordinator run failed", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.reports[runID] = report
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id query parameter required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	report, ok := s.reports[id]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
