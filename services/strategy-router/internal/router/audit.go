package router

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// FileAudit appends one JSON line per decision to the given path, plus a
// human-readable text mirror that matches scripts/strategy-regime-router.sh
// so existing Grafana / log-tail tooling keeps working.
type FileAudit struct {
	path string
	mu   sync.Mutex
}

// NewFileAudit returns a FileAudit that writes to `path`. Set path to "" or
// "stdout" to write to standard output instead of a file.
func NewFileAudit(path string) *FileAudit {
	return &FileAudit{path: path}
}

// WriteDecision appends one JSON line for the decision and, when the
// action mutates lifecycle (switched|started|stopped|paused), a second
// human-readable line in the bash-router format.
func (f *FileAudit) WriteDecision(d Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	w, closer, err := f.writer()
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	line, _ := json.Marshal(d)
	if _, err := fmt.Fprintln(w, string(line)); err != nil {
		return err
	}

	switch d.Action {
	case "switched", "started":
		_, err = fmt.Fprintf(w, "%s regime=%s stop=%s start=%s\n",
			d.Timestamp.UTC().Format(time.RFC3339), d.Regime, displayOrNone(d.Current), d.Preferred)
	case "paused":
		_, err = fmt.Fprintf(w, "%s regime=%s → pause stop=%s\n",
			d.Timestamp.UTC().Format(time.RFC3339), d.Regime, displayOrNone(d.Current))
	}
	return err
}

func (f *FileAudit) writer() (io.Writer, io.Closer, error) {
	if f.path == "" || f.path == "stdout" {
		return os.Stdout, nil, nil
	}
	fp, err := os.OpenFile(f.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return fp, fp, nil
}

func displayOrNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
