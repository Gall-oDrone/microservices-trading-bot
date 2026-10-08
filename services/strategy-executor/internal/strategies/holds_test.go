package strategies

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

func newHoldTestRegistry(t *testing.T, names ...string) *EnhancedRegistry {
	t.Helper()
	svc := indicators.NewService(nil, indicators.NewInMemoryIndicatorStore(), indicators.NewMockDataProvider(), nil)
	reg := NewEnhancedRegistry(svc)
	for _, n := range names {
		if _, err := reg.CreateAndRegister(StrategyConfig{Name: n, Type: "mean_reversion", Enabled: true, Book: "btc_mxn"}); err != nil {
			t.Fatalf("CreateAndRegister(%s): %v", n, err)
		}
	}
	return reg
}

func TestLoadHoldList_MissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "strategy-holds.json")
	h, err := LoadHoldList(path)
	if err != nil {
		t.Fatalf("LoadHoldList: %v", err)
	}
	if len(h.All()) != 0 {
		t.Fatalf("want no holds, got %v", h.All())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("loading must not create the file, stat err=%v", err)
	}
}

func TestLoadHoldList_CorruptFileIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strategy-holds.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHoldList(path); err == nil {
		t.Fatal("want an error for a corrupt hold list")
	}
}

func TestHoldList_PersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "strategy-holds.json")
	h, err := LoadHoldList(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	if err := h.Put("mr_btc", HoldEntry{Reason: "drift", By: "diego", At: at}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := h.Put("mom_eth", HoldEntry{Reason: "test", By: "diego", At: at}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := h.Delete("mom_eth"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := h.Delete("never_held"); err != nil {
		t.Fatalf("Delete of a missing name must be a no-op: %v", err)
	}

	again, err := LoadHoldList(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := again.Get("mr_btc")
	if !ok || got.Reason != "drift" || got.By != "diego" || !got.At.Equal(at) {
		t.Fatalf("reloaded hold = %+v, %v", got, ok)
	}
	if names := again.Names(); len(names) != 1 || names[0] != "mr_btc" {
		t.Fatalf("names = %v", names)
	}
	// No temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("want only the hold file in the dir, got %d entries", len(entries))
	}
}

func TestHoldList_WriteFailureLeavesMemoryUnchanged(t *testing.T) {
	dir := t.TempDir()
	// The parent "directory" is a regular file, so every write fails.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := &HoldList{path: filepath.Join(blocker, "strategy-holds.json"), holds: map[string]HoldEntry{}}
	if err := h.Put("x", HoldEntry{By: "a"}); err == nil {
		t.Fatal("want a write error")
	}
	if _, ok := h.Get("x"); ok {
		t.Fatal("a failed Put must not change the in-memory list")
	}
}

func TestRegistry_HoldBlocksPlainStart(t *testing.T) {
	ctx := context.Background()
	reg := newHoldTestRegistry(t, "s1")
	if err := reg.Start(ctx, "s1"); err != nil {
		t.Fatal(err)
	}

	wasRunning, err := reg.Hold("s1", HoldEntry{By: "diego", Reason: "investigate"})
	if err != nil || !wasRunning {
		t.Fatalf("Hold = %v, %v; want true, nil", wasRunning, err)
	}
	info, _ := reg.GetStrategyInfo("s1")
	if info.Running || info.Hold == nil || info.Hold.By != "diego" || info.Hold.At.IsZero() {
		t.Fatalf("info after hold = running %v hold %+v", info.Running, info.Hold)
	}

	if err := reg.Start(ctx, "s1"); !errors.Is(err, ErrStrategyHeld) {
		t.Fatalf("plain Start on a held strategy = %v, want ErrStrategyHeld", err)
	}
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if info, _ := reg.GetStrategyInfo("s1"); info.Running {
		t.Fatal("StartAll must skip held strategies")
	}

	// Holding again while stopped just refreshes the hold.
	if wasRunning, err := reg.Hold("s1", HoldEntry{By: "ops", Reason: "again"}); err != nil || wasRunning {
		t.Fatalf("second Hold = %v, %v", wasRunning, err)
	}

	released, err := reg.StartReleasingHold(ctx, "s1")
	if err != nil {
		t.Fatalf("StartReleasingHold: %v", err)
	}
	if released == nil || released.By != "ops" {
		t.Fatalf("released = %+v", released)
	}
	info, _ = reg.GetStrategyInfo("s1")
	if !info.Running || info.Hold != nil {
		t.Fatalf("after release: running %v hold %+v", info.Running, info.Hold)
	}
	if len(reg.Holds()) != 0 {
		t.Fatalf("holds = %v", reg.Holds())
	}
}

func TestRegistry_LifecycleErrorKinds(t *testing.T) {
	ctx := context.Background()
	reg := newHoldTestRegistry(t, "s1")

	if err := reg.Start(ctx, "nope"); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("Start unknown = %v", err)
	}
	if _, err := reg.Hold("nope", HoldEntry{}); !errors.Is(err, ErrStrategyNotFound) {
		t.Fatalf("Hold unknown = %v", err)
	}
	if err := reg.Stop("s1"); !errors.Is(err, ErrStrategyNotRunning) {
		t.Fatalf("Stop stopped = %v", err)
	}
	if err := reg.Start(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Start(ctx, "s1"); !errors.Is(err, ErrStrategyRunning) {
		t.Fatalf("Start running = %v", err)
	}
	if _, err := reg.StartReleasingHold(ctx, "s1"); !errors.Is(err, ErrStrategyRunning) {
		t.Fatalf("StartReleasingHold running = %v", err)
	}
	// Messages are unchanged for existing callers and logs.
	if err := reg.Start(ctx, "s1"); err.Error() != "strategy 's1' is already running" {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestRegistry_HoldSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "strategy-holds.json")

	first := newHoldTestRegistry(t, "s1", "s2")
	holds, err := LoadHoldList(path)
	if err != nil {
		t.Fatal(err)
	}
	first.SetHoldList(holds)
	if _, err := first.Hold("s1", HoldEntry{By: "diego", Reason: "keep off"}); err != nil {
		t.Fatal(err)
	}

	// "Restart": a fresh registry re-registers the same strategies and loads
	// the same file, as main.go does at boot.
	second := newHoldTestRegistry(t, "s1", "s2")
	reloaded, err := LoadHoldList(path)
	if err != nil {
		t.Fatal(err)
	}
	second.SetHoldList(reloaded)
	if err := second.Start(ctx, "s1"); !errors.Is(err, ErrStrategyHeld) {
		t.Fatalf("s1 after restart = %v, want held", err)
	}
	if err := second.Start(ctx, "s2"); err != nil {
		t.Fatalf("s2 must still start: %v", err)
	}
}
