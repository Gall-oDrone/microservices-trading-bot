package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/prereg"
)

// Plan §6.4.14: the latest pre-registered verdict report (H1/H2/H3) for a
// book, as written by scripts/prereg-evaluation.sh (weekly via
// scripts/ops-run.sh prereg, or committed under docs evidence). Reporting
// only: before 2027-09-26 the report decides nothing.

// PreregResponse is GET /api/ui/forward-tests/{book}/prereg.
type PreregResponse struct {
	Book        string `json:"book"`
	GeneratedAt string `json:"generated_at"`
	Found       bool   `json:"found"`
	Source      string `json:"source"` // the report file, relative to the directory it was found in
	// From the report.
	Phase             string              `json:"phase"`
	AsOf              string              `json:"as_of"`
	ReportGeneratedAt string              `json:"report_generated_at"`
	Decides           bool                `json:"decides"`
	Note              string              `json:"note"`
	Verdict           *prereg.BookVerdict `json:"verdict"` // null when the report has no such book
	// The frozen dates and the days left (negative once passed).
	InterimDate   string `json:"interim_date"`
	FinalDate     string `json:"final_date"`
	DaysToInterim int    `json:"days_to_interim"`
	DaysToFinal   int    `json:"days_to_final"`
	Skipped       int    `json:"skipped_files"` // unreadable or other-schema report files
}

func daysUntil(now time.Time, date string) int {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return int(d.Sub(today).Hours() / 24)
}

func (s *Server) preregReport(w http.ResponseWriter, r *http.Request) {
	b := r.PathValue("book")
	if !bookRe.MatchString(b) {
		writeErr(w, http.StatusBadRequest, "invalid book")
		return
	}
	now := s.Now().UTC()
	resp := PreregResponse{Book: b, GeneratedAt: now.Format(time.RFC3339),
		InterimDate: prereg.InterimDate, FinalDate: prereg.FinalDate,
		DaysToInterim: daysUntil(now, prereg.InterimDate), DaysToFinal: daysUntil(now, prereg.FinalDate)}
	rep, path, found, skipped, err := prereg.Latest(s.PreregDirs)
	if err != nil {
		s.Log.Printf("prereg: %v", err)
	}
	resp.Skipped = skipped
	if found {
		resp.Found = true
		resp.Source = filepath.Base(path)
		for _, root := range s.PreregDirs {
			if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
				resp.Source = filepath.ToSlash(rel)
				break
			}
		}
		resp.Phase, resp.AsOf, resp.ReportGeneratedAt, resp.Decides, resp.Note = rep.Phase, rep.AsOf, rep.GeneratedAt, rep.Decides, rep.Note
		if v, ok := rep.Book(b); ok {
			resp.Verdict = &v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
