// Package stageenv loads the Bitso stage credentials file shared by the
// daily-executor and daily-reconcile (moved verbatim from
// cmd/daily-executor/stage.go, which now delegates here).
package stageenv

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bitso-trading-platform/strategy-executor/internal/bitsostage"
)

// LoadFile sets KEY=VALUE pairs from path for keys not already set in the
// environment. A missing file is not an error. Values are never printed.
func LoadFile(path string) error {
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

// DefaultFile is ~/.config/microservices-trading-bot/bitso-stage.env.
func DefaultFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "bitso-stage.env")
}

// Client builds the stage client from the environment: STAGE_BITSO_API_KEY,
// STAGE_BITSO_API_SECRET (or the legacy STAGE_BITSO_APISECRET) and an
// optional BITSO_API_BASE_URL, which bitsostage.New only accepts if it is
// the stage URL.
func Client() (*bitsostage.Client, error) {
	secret := os.Getenv("STAGE_BITSO_API_SECRET")
	if secret == "" {
		secret = os.Getenv("STAGE_BITSO_APISECRET") // legacy name used by order-management
	}
	base := os.Getenv("BITSO_API_BASE_URL")
	if base == "" {
		base = bitsostage.StageBaseURL
	}
	return bitsostage.New(base, os.Getenv("STAGE_BITSO_API_KEY"), secret)
}
