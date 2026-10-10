package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"bitso-trading-platform/news-publisher/internal/ingest"
	"bitso-trading-platform/news-publisher/internal/state"
)

type Server struct {
	store *state.Store
}

func New(store *state.Store) *Server {
	return &Server{store: store}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/api/v1/news/sentiment", s.sentiment)
	mux.HandleFunc("/api/v1/news/snapshot", s.snapshot)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) sentiment(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if symbol == "" {
		http.Error(w, "symbol query param required", http.StatusBadRequest)
		return
	}
	evt, ok := ingest.SentimentForSymbol(s.store, symbol)
	if !ok {
		http.Error(w, "no sentiment cached yet", http.StatusNotFound)
		return
	}
	writeJSON(w, evt)
}

func (s *Server) snapshot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{
		"last_s3_object_key": s.store.LastObjectKey(),
		"symbols":            s.store.Snapshot(),
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
