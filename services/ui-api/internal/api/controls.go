package api

// Operator controls (R4): halt and resume the daily-executor from the UI by
// writing the ledger's halt file (risk-state.json, R2), with an append-only
// audit log next to it.
//
// Until Phase 4 auth (OIDC) exists this is deliberately narrow:
//   - off unless ui-api is started with an operator token (-operator-token-file),
//     and then only on a loopback address;
//   - every write needs "Authorization: Bearer <token>", a JSON body with a
//     reason, who, and the ledger name typed again as confirmation;
//   - a browser Origin, when sent, must be a loopback origin;
//   - only local ledgers (the file the executor reads), never an S3 copy;
//   - every attempt is audited, and nothing changes unless the "requested"
//     audit line was written first.
//
// The executor is unchanged: it already reads the halt file before every run
// and fails closed on an invalid one.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
)

const (
	maxControlBody = 4 << 10
	minReasonLen   = 8
	maxReasonLen   = 500
	// auditTail is how many audit entries GET /controls returns.
	auditTail = 50
)

// controlPaths are the only routes that accept POST; everything else stays
// read-only (405).
var controlPaths = map[string]bool{
	"/api/ui/risk/halt":   true,
	"/api/ui/risk/resume": true,
}

var byRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$`)

// ControlRequest is the body of POST /api/ui/risk/halt and /resume.
type ControlRequest struct {
	Reason string `json:"reason"`
	By     string `json:"by"`
	// Confirm must equal the ledger name, so a halt is never sent to the
	// wrong ledger by a stale page.
	Confirm string `json:"confirm"`
}

// ControlResponse is the answer to a successful halt or resume.
type ControlResponse struct {
	Ledger   string       `json:"ledger"`
	Action   string       `json:"action"`
	HaltFile HaltFileInfo `json:"halt_file"`
	Audit    audit.Entry  `json:"audit"`
	// AuditError is set when the change was made but its "done" line could
	// not be written (the "requested" line was).
	AuditError string `json:"audit_error,omitempty"`
}

// ControlsInfo is GET /api/ui/controls: whether controls are available for
// this ledger, the halt file, and the recent audit log.
type ControlsInfo struct {
	Ledger  string `json:"ledger"`
	Enabled bool   `json:"enabled"`
	// DisabledReason says why controls are off for this ledger.
	DisabledReason string        `json:"disabled_reason,omitempty"`
	HaltFile       HaltFileInfo  `json:"halt_file"`
	AuditPath      string        `json:"audit_path"`
	Audit          []audit.Entry `json:"audit"`
	AuditError     string        `json:"audit_error,omitempty"`
}

// controlsState reports whether a ledger can be controlled from the UI.
func (s *Server) controlsState(l Ledger) (bool, string) {
	if s.OperatorToken == "" {
		return false, "controls are off: start ui-api with -operator-token-file (see the plan, §8.12)"
	}
	if l.Store.Remote() {
		return false, "this ledger is read from S3; controls act on the local ledger the executor reads"
	}
	return true, ""
}

// auditPath is the audit log for a ledger ("" for an S3 ledger).
func auditPath(l Ledger) string {
	if l.Store.Remote() {
		return ""
	}
	return filepath.Join(filepath.Dir(l.Store.LedgerPath), audit.FileName)
}

func (s *Server) controls(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	enabled, why := s.controlsState(l)
	_, hf := s.policyFor(l)
	info := ControlsInfo{Ledger: l.Name, Enabled: enabled, DisabledReason: why, HaltFile: hf, Audit: []audit.Entry{}}
	if p := auditPath(l); p != "" {
		info.AuditPath = p
		entries, err := audit.Tail(p, auditTail)
		info.Audit = entries
		if err != nil {
			info.AuditError = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) haltAction(w http.ResponseWriter, r *http.Request)   { s.control(w, r, "halt") }
func (s *Server) resumeAction(w http.ResponseWriter, r *http.Request) { s.control(w, r, "resume") }

// authorized checks the bearer token in constant time.
func (s *Server) authorized(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" || s.OperatorToken == "" {
		return false
	}
	a := sha256.Sum256([]byte(tok))
	b := sha256.Sum256([]byte(s.OperatorToken))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// loopbackOrigin accepts no Origin (curl) or a loopback one (the UI on
// 127.0.0.1:5173 through the Vite proxy, or served by ui-api itself).
func loopbackOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func validateControl(req ControlRequest, ledger string) error {
	reason := strings.TrimSpace(req.Reason)
	switch {
	case len([]rune(reason)) < minReasonLen:
		return fmt.Errorf("reason: at least %d characters, so the halt is explained in the ledger", minReasonLen)
	case len([]rune(reason)) > maxReasonLen:
		return fmt.Errorf("reason: at most %d characters", maxReasonLen)
	case strings.ContainsAny(reason, "\r\n\x00"):
		return errors.New("reason: one line, no control characters")
	case !byRe.MatchString(strings.TrimSpace(req.By)):
		return errors.New("by: 1-64 letters, digits, spaces or . _ @ -")
	case req.Confirm != ledger:
		return fmt.Errorf("confirm: type the ledger name %q to confirm", ledger)
	}
	return nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Server) control(w http.ResponseWriter, r *http.Request, action string) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	enabled, why := s.controlsState(l)
	if !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := auditPath(l)
	now := s.Now().UTC()
	base := audit.Entry{At: now.Format(time.RFC3339), Action: action, Ledger: l.Name,
		Remote: r.RemoteAddr, UserAgent: clip(r.UserAgent(), 200)}
	// note writes a line for an attempt that changes nothing.
	note := func(e audit.Entry) {
		if err := audit.Append(ap, e); err != nil {
			s.Log.Printf("audit %s: %v", ap, err)
		}
	}
	deny := func(status int, msg string) {
		e := base
		e.Outcome, e.Error = audit.Denied, msg
		note(e)
		writeErr(w, status, msg)
	}
	if !loopbackOrigin(r) {
		deny(http.StatusForbidden, "origin "+clip(r.Header.Get("Origin"), 100)+" may not use controls")
		return
	}
	if !s.authorized(r) {
		deny(http.StatusUnauthorized, "missing or wrong operator token")
		return
	}
	refuse := func(e audit.Entry, status int, msg string) {
		e.Outcome, e.Error = audit.Refused, msg
		note(e)
		writeErr(w, status, msg)
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		refuse(base, http.StatusUnsupportedMediaType, "send Content-Type: application/json")
		return
	}
	var req ControlRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxControlBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		refuse(base, http.StatusBadRequest, "body: "+err.Error())
		return
	}
	base.By, base.Reason = clip(strings.TrimSpace(req.By), 64), clip(strings.TrimSpace(req.Reason), maxReasonLen)
	if err := validateControl(req, l.Name); err != nil {
		refuse(base, http.StatusBadRequest, err.Error())
		return
	}

	// One control at a time: read, decide and write without interleaving.
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	cur, _, curErr := l.Store.Halt()
	if curErr == nil {
		base.Before = &cur
	}
	switch {
	case action == "halt" && curErr == nil && cur.Halted:
		refuse(base, http.StatusConflict, fmt.Sprintf("already halted by %s at %s: %s", cur.By, cur.At, cur.Reason))
		return
	case action == "resume" && curErr != nil:
		// Fail closed: the executor refuses to run on an invalid file; lifting
		// that is a hand edit, not a button.
		refuse(base, http.StatusConflict, "the halt file is invalid ("+curErr.Error()+"); fix or remove it by hand")
		return
	case action == "resume" && !cur.Halted:
		refuse(base, http.StatusConflict, "not halted by the halt file; nothing to resume")
		return
	}

	base.ID = newID()
	req1 := base
	req1.Outcome = audit.Requested
	if curErr != nil {
		req1.Error = "replacing an invalid halt file: " + curErr.Error()
	}
	if err := audit.Append(ap, req1); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		writeErr(w, http.StatusInternalServerError, "cannot write the audit log ("+err.Error()+"); nothing changed")
		return
	}
	next := risk.HaltState{Halted: action == "halt", Reason: base.Reason, By: base.By, At: base.At}
	done := base
	if err := writeHaltFile(l.Store.HaltPath(), next); err != nil {
		done.Outcome, done.Error = audit.Failed, err.Error()
		note(done)
		writeErr(w, http.StatusInternalServerError, "could not write the halt file: "+err.Error())
		return
	}
	done.Outcome, done.After = audit.Done, &next
	resp := ControlResponse{Ledger: l.Name, Action: action, Audit: done}
	if err := audit.Append(ap, done); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		resp.AuditError = err.Error()
	}
	_, resp.HaltFile = s.policyFor(l)
	s.Log.Printf("control %s ledger=%s by=%q id=%s", action, l.Name, base.By, base.ID)
	writeJSON(w, http.StatusOK, resp)
}

// writeHaltFile replaces the halt file atomically (temp file in the same
// dir, fsync, rename), so the executor never reads a partial file. The
// content is checked with the executor's own parser first.
func writeHaltFile(path string, h risk.HaltState) error {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := risk.ParseHaltState(b); err != nil {
		return fmt.Errorf("refusing to write an invalid halt file: %w", err)
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".risk-state-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// LoadOperatorToken reads the operator token file. It must be readable by
// its owner only (0600 or stricter) and hold at least 32 characters.
func LoadOperatorToken(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%s is mode %04o: chmod 600 it (the token controls trading)", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if len(tok) < 32 || strings.ContainsAny(tok, " \t\r\n") {
		return "", fmt.Errorf("%s: want one token of at least 32 characters (e.g. openssl rand -hex 32)", path)
	}
	return tok, nil
}
