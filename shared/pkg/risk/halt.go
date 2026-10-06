package risk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HaltFileName is the operator halt file, kept next to a ledger. The
// daily-executor reads it before every run; ui-api shows it read-only.
const HaltFileName = "risk-state.json"

// HaltState is the content of the halt file. An operator writes it by hand
// (until the UI can, R4) to stop new orders without touching the policy:
//
//	{"halted": true, "reason": "exchange incident", "by": "diego", "at": "2026-10-06T01:00:00Z"}
//
// `{"halted": false}` (or no file) means not halted. Who, why and when are
// required while halted, so the halt is always explained in the ledger.
type HaltState struct {
	Halted bool   `json:"halted"`
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
	At     string `json:"at,omitempty"` // RFC 3339
}

// HaltPath is the halt file that belongs to a ledger.
func HaltPath(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), HaltFileName)
}

// Validate requires reason, by and an RFC 3339 time while halted.
func (h HaltState) Validate() error {
	if !h.Halted {
		return nil
	}
	var missing []string
	if strings.TrimSpace(h.Reason) == "" {
		missing = append(missing, "reason")
	}
	if strings.TrimSpace(h.By) == "" {
		missing = append(missing, "by")
	}
	if strings.TrimSpace(h.At) == "" {
		missing = append(missing, "at")
	} else if _, err := time.Parse(time.RFC3339, h.At); err != nil {
		return fmt.Errorf("at %q is not RFC 3339", h.At)
	}
	if len(missing) > 0 {
		return fmt.Errorf("halted needs %s", strings.Join(missing, ", "))
	}
	return nil
}

// LoadHaltState reads a halt file. A missing file returns found=false and no
// error. A file that exists but cannot be read or parsed, has unknown fields,
// or fails Validate is an error: callers that trade must then refuse to run
// (fail closed), because the operator meant something by writing it.
func LoadHaltState(path string) (h HaltState, found bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return HaltState{}, false, nil
	}
	if err != nil {
		return HaltState{}, true, fmt.Errorf("halt file %s: %w", path, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return HaltState{}, true, fmt.Errorf("halt file %s: %w", path, err)
	}
	if err := h.Validate(); err != nil {
		return HaltState{}, true, fmt.Errorf("halt file %s: %w", path, err)
	}
	return h, true, nil
}

// ApplyHalt returns p with the halt file merged into its global halt. The
// policy's own halt is kept; when both are set, both reasons are shown.
func ApplyHalt(p Policy, h HaltState) Policy {
	if !h.Halted {
		return p
	}
	msg := fmt.Sprintf("%s (halt file, by %s at %s)", h.Reason, h.By, h.At)
	if p.Halted && p.HaltReason != "" {
		msg = p.HaltReason + "; " + msg
	}
	p.Halted, p.HaltReason = true, msg
	return p
}
