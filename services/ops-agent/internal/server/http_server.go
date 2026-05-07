package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	agentruntime "bitso-trading-platform/ops-agent/internal/agent"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type alertmanagerPayload struct {
	Status string `json:"status"`
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"alerts"`
}

type Server struct {
	httpServer *http.Server
	agent      *agentruntime.OpsAgent

	mu      sync.RWMutex
	reports map[string]agentruntime.Report

	runsTotal   prometheus.Counter
	runsFailed  prometheus.Counter
	runDuration prometheus.Histogram
}

func New(addr string, agent *agentruntime.OpsAgent) *Server {
	runsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_runs_total",
		Help: "Total number of ops-agent runs",
	})
	runsFailed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_failures_total",
		Help: "Total number of failed ops-agent runs",
	})
	runDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "agent_run_latency_ms",
		Help:    "Ops-agent run latency in milliseconds",
		Buckets: []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000},
	})
	prometheus.MustRegister(runsTotal, runsFailed, runDuration)

	s := &Server{
		agent:       agent,
		reports:     map[string]agentruntime.Report{},
		runsTotal:   runsTotal,
		runsFailed:  runsFailed,
		runDuration: runDuration,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/v1/ops-agent/run", s.handleRunManual)
	mux.HandleFunc("/api/v1/ops-agent/report", s.handleGetReport)
	mux.HandleFunc("/api/v1/alerts/alertmanager", s.handleAlertmanagerWebhook)

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
	_, _ = w.Write([]byte(`{"status":"ok","service":"ops-agent"}`))
}

func (s *Server) handleRunManual(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var incident agentruntime.Incident
	if err := json.NewDecoder(r.Body).Decode(&incident); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}

	reportID := fmt.Sprintf("manual-%d", time.Now().UTC().UnixNano())
	report, err := s.runTriage(r.Context(), reportID, incident)
	if err != nil {
		http.Error(w, "triage failed", http.StatusInternalServerError)
		return
	}
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

func (s *Server) handleAlertmanagerWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload alertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request payload", http.StatusBadRequest)
		return
	}
	if len(payload.Alerts) == 0 {
		http.Error(w, "no alerts in payload", http.StatusBadRequest)
		return
	}

	first := payload.Alerts[0]
	incident := agentruntime.Incident{
		Source:      "alertmanager",
		Severity:    first.Labels["severity"],
		Title:       first.Annotations["summary"],
		Description: first.Annotations["description"],
		Labels:      first.Labels,
	}
	if incident.Title == "" {
		incident.Title = first.Labels["alertname"]
	}
	if incident.Severity == "" {
		incident.Severity = "unknown"
	}

	reportID := fmt.Sprintf("alert-%d", time.Now().UTC().UnixNano())
	report, err := s.runTriage(r.Context(), reportID, incident)
	if err != nil {
		http.Error(w, "triage failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"report_id": report.ID,
		"status":    "triaged",
	})
}

func (s *Server) runTriage(ctx context.Context, reportID string, incident agentruntime.Incident) (agentruntime.Report, error) {
	start := time.Now()
	s.runsTotal.Inc()

	report, err := s.agent.TriageIncident(ctx, reportID, incident)
	durationMS := float64(time.Since(start).Milliseconds())
	s.runDuration.Observe(durationMS)
	if err != nil {
		s.runsFailed.Inc()
		return agentruntime.Report{}, err
	}

	s.mu.Lock()
	s.reports[report.ID] = report
	s.mu.Unlock()
	return report, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
