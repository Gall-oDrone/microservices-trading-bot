// Package server exposes the HTTP control surface for strategy-router.
//
// Endpoints:
//
//	GET  /health                  — liveness probe
//	GET  /metrics                 — Prometheus exposition
//	GET  /api/v1/router/state     — current regime + active strategy + recent decisions
//	POST /api/v1/router/run       — run one evaluation cycle on demand
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"bitso-trading-platform/strategy-router/internal/config"
	"bitso-trading-platform/strategy-router/internal/router"
)

// Server is the HTTP frontend for the router engine.
type Server struct {
	cfg        *config.Config
	engine     *router.Engine
	httpServer *http.Server
}

// New builds a Server bound to addr.
func New(addr string, cfg *config.Config, engine *router.Engine) *Server {
	s := &Server{
		cfg:    cfg,
		engine: engine,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/v1/router/state", s.handleState)
	mux.HandleFunc("/api/v1/router/run", s.handleRunOnce)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Start blocks until the server stops.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Stop gracefully shuts the server down.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Book    string `json:"book"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "healthy",
		Service: s.cfg.ServiceName,
		Book:    s.cfg.Book,
	})
}

type stateResponse struct {
	Book            string             `json:"book"`
	DryRun          bool               `json:"dry_run"`
	Routes          config.RouteTable  `json:"routes"`
	Last            router.Decision    `json:"last_decision"`
	Recent          []router.Decision  `json:"recent_decisions"`
	CooldownSeconds int                `json:"cooldown_seconds"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, stateResponse{
		Book:            s.cfg.Book,
		DryRun:          s.cfg.DryRun,
		Routes:          s.cfg.Routes,
		Last:            s.engine.LastDecision(),
		Recent:          s.engine.RecentDecisions(),
		CooldownSeconds: s.cfg.CooldownSeconds,
	})
}

func (s *Server) handleRunOnce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, err := s.engine.RunOnce(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
