package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"bitso-trading-platform/strategy-executor/internal/dailyexec"
)

// position is what the executor itself holds on stage for one book. It is
// tracked from the executor's own fills (recorded in the ledger), not from
// account balances: the stage account holds BTC that has nothing to do with
// the strategy, and both books draw on the same BTC balance.
type position struct {
	State string  `json:"state"` // "long" | "flat"
	BTC   float64 `json:"btc"`
}

// stageInfo is the stage part of a ledger record.
type stageInfo struct {
	Env            string            `json:"env"`
	Target         string            `json:"target"` // the rule's signal
	Action         string            `json:"action"` // "buy" | "sell" | "none"
	PositionBefore position          `json:"position_before"`
	PositionAfter  position          `json:"position_after"`
	Leg            *dailyexec.Result `json:"leg,omitempty"`
}

const btcDust = 1e-8

// maxSize caps the BTC per order while this runs on stage with test money.
const maxSize = 0.01

func flatPos() position { return position{State: "flat"} }

// lastStagePosition is the position after the most recent stage record.
func lastStagePosition(l *ledger, book string) position {
	var dates []string
	for _, r := range l.entries {
		if r.Book == book && r.Mode == "stage" && r.Stage != nil {
			dates = append(dates, r.Decision.BarDate)
		}
	}
	if len(dates) == 0 {
		return flatPos()
	}
	sort.Strings(dates)
	r, _ := l.get(book, dates[len(dates)-1])
	return r.Stage.PositionAfter
}

// planAction turns the rule's target and the current position into an order.
func planAction(target string, pos position, size float64) (action string, qty float64) {
	switch {
	case target == "long" && pos.BTC <= btcDust:
		return "buy", size
	case target == "flat" && pos.BTC > btcDust:
		return "sell", pos.BTC
	default:
		return "none", 0
	}
}

// applyFill returns the position after a leg.
func applyFill(pos position, action string, filled float64) position {
	btc := pos.BTC
	switch action {
	case "buy":
		btc += filled
	case "sell":
		btc -= filled
	}
	if btc < btcDust {
		return position{State: "flat", BTC: 0}
	}
	return position{State: "long", BTC: btc}
}

// loadEnvFile sets KEY=VALUE pairs from path for keys not already set in the
// environment. A missing file is not an error. Values are never printed.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by group/others (mode %v); run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func defaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "bitso-stage.env")
}

// lockFile takes an exclusive, non-blocking lock so two runs (e.g. a timer
// and a manual run) can never trade at the same time. Released on exit.
func lockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another daily-executor run holds %s", path)
	}
	return f, nil
}
