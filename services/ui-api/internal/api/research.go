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

// RunsResponse is GET /api/ui/research/runs.
type RunsResponse struct {
	GeneratedAt string                `json:"generated_at"`
	Dir         string                `json:"dir"`
	Found       bool                  `json:"found"`
	Runs        []research.Run        `json:"runs"`
	Skipped     []research.SkippedRun `json:"skipped"` // report files that could not be read
}

// runs lists the research tools' JSON reports (research-run/*) found in the
// evidence directories, newest first.
func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	resp := RunsResponse{GeneratedAt: s.Now().UTC().Format(time.RFC3339), Runs: []research.Run{}, Skipped: []research.SkippedRun{}}
	if s.Research == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Dir = s.Research.RepoRel
	runs, skipped, found, err := s.Research.Runs()
	if err != nil {
		s.Log.Printf("research runs: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read the evidence folders")
		return
	}
	resp.Found, resp.Runs, resp.Skipped = found, runs, skipped
	writeJSON(w, http.StatusOK, resp)
}

// run is GET /api/ui/research/runs/{date}/{name}: one report, passed through unchanged.
func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	date, name := r.PathValue("date"), r.PathValue("name")
	if !research.ValidRunID(date, name) {
		writeErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	if s.Research == nil {
		writeErr(w, http.StatusNotFound, "no studies folder configured")
		return
	}
	d, ok, err := s.Research.GetRun(date, name)
	if err != nil {
		s.Log.Printf("research run %s/%s: %v", date, name, err)
		writeErr(w, http.StatusInternalServerError, "could not read run "+date+"/"+name)
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "no run "+date+"/"+name)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
