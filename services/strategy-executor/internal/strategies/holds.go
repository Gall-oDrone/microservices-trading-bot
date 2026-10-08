package strategies

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// HoldEntry records why an operator stopped a strategy and asked it to stay
// stopped. A held strategy refuses plain start requests (from the router, the
// organic-trading script, or StartAll) until an operator releases it.
type HoldEntry struct {
	Reason string    `json:"reason"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

// holdFile is the on-disk shape of the hold list. Version lets later readers
// recognise the format; unknown fields are ignored on read.
type holdFile struct {
	Version int                  `json:"version"`
	Holds   map[string]HoldEntry `json:"holds"`
}

// HoldList is the operator stop list, optionally persisted to a JSON file so
// that a stopped strategy stays stopped across restarts (same idea as the
// daily executor's risk-state.json). With an empty path it is memory-only.
type HoldList struct {
	path  string
	mu    sync.Mutex
	holds map[string]HoldEntry
}

// NewMemoryHoldList returns a hold list that is not persisted.
func NewMemoryHoldList() *HoldList {
	return &HoldList{holds: map[string]HoldEntry{}}
}

// LoadHoldList reads path (a missing file means "no holds") and returns a list
// that writes back to the same path on every change. A file that exists but
// cannot be parsed is an error: silently dropping holds would let a stopped
// strategy start again.
func LoadHoldList(path string) (*HoldList, error) {
	h := &HoldList{path: path, holds: map[string]HoldEntry{}}
	if path == "" {
		return h, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hold list %s: %w", path, err)
	}
	var f holdFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse hold list %s: %w", path, err)
	}
	for name, e := range f.Holds {
		if name != "" {
			h.holds[name] = e
		}
	}
	return h, nil
}

// Path returns the backing file ("" when memory-only).
func (h *HoldList) Path() string { return h.path }

// Get returns the hold for name, if any.
func (h *HoldList) Get(name string) (HoldEntry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.holds[name]
	return e, ok
}

// All returns a copy of every hold.
func (h *HoldList) All() map[string]HoldEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]HoldEntry, len(h.holds))
	for k, v := range h.holds {
		out[k] = v
	}
	return out
}

// Names returns the held strategy names, sorted.
func (h *HoldList) Names() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	names := make([]string, 0, len(h.holds))
	for k := range h.holds {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Put adds or replaces the hold for name and persists it. On a write error the
// in-memory list is left unchanged.
func (h *HoldList) Put(name string, e HoldEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := make(map[string]HoldEntry, len(h.holds)+1)
	for k, v := range h.holds {
		next[k] = v
	}
	next[name] = e
	if err := h.persist(next); err != nil {
		return err
	}
	h.holds = next
	return nil
}

// Delete removes the hold for name and persists the change. Deleting a name
// that is not held is a no-op.
func (h *HoldList) Delete(name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.holds[name]; !ok {
		return nil
	}
	next := make(map[string]HoldEntry, len(h.holds))
	for k, v := range h.holds {
		if k != name {
			next[k] = v
		}
	}
	if err := h.persist(next); err != nil {
		return err
	}
	h.holds = next
	return nil
}

// persist writes holds atomically (temp file in the same directory, fsync,
// rename). Must be called with h.mu held.
func (h *HoldList) persist(holds map[string]HoldEntry) error {
	if h.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(holdFile{Version: 1, Holds: holds}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode hold list: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create hold list dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".strategy-holds-*.json")
	if err != nil {
		return fmt.Errorf("create temp hold list: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write hold list: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync hold list: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close hold list: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod hold list: %w", err)
	}
	if err := os.Rename(tmpName, h.path); err != nil {
		return fmt.Errorf("replace hold list: %w", err)
	}
	return nil
}
