package api

import (
	"net/http"
	"time"

	"bitso-trading-platform/ui-api/internal/research"
)

// StudiesResponse is GET /api/ui/research/studies.
type StudiesResponse struct {
	GeneratedAt string           `json:"generated_at"`
	Dir         string           `json:"dir"`   // repo-relative folder the studies come from
	Found       bool             `json:"found"` // false when the folder does not exist
	Studies     []research.Study `json:"studies"`
}

// studies lists the study write-ups, newest first.
func (s *Server) studies(w http.ResponseWriter, r *http.Request) {
	resp := StudiesResponse{GeneratedAt: s.Now().UTC().Format(time.RFC3339), Studies: []research.Study{}}
	if s.Research == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Dir = s.Research.RepoRel
	list, found, err := s.Research.List()
	if err != nil {
		s.Log.Printf("research: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read the studies folder")
		return
	}
	resp.Found, resp.Studies = found, list
	writeJSON(w, http.StatusOK, resp)
}

// study is GET /api/ui/research/studies/{name}: one study rendered to HTML
// (raw HTML in the markdown is dropped), with its table of contents.
func (s *Server) study(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !research.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "invalid study name")
		return
	}
	if s.Research == nil {
		writeErr(w, http.StatusNotFound, "no studies folder configured")
		return
	}
	d, ok, err := s.Research.Get(name)
	if err != nil {
		s.Log.Printf("research %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not read study "+name)
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no study "+name)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
