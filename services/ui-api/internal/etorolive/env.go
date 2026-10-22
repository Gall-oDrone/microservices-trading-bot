package etorolive

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"bitso-trading-platform/shared/pkg/etoro"
)

// DefaultEnvFile is ~/.config/microservices-trading-bot/etoro-demo.env, the
// file the executor, reconcile and the risk report read their demo keys
// from (strategy-executor's loader is internal to that module; this one
// reads the same KEY=VALUE format and never exports or prints a value).
func DefaultEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "etoro-demo.env")
}

// ErrNotDemo: the file is not for the demo environment. The console never
// holds real-account keys, even for a read-only rates poll.
var ErrNotDemo = errors.New("ETORO_ENV must be demo")

// LoadEnvFile parses KEY=VALUE lines (blank lines and # comments skipped,
// surrounding quotes trimmed) into a map. The values stay in the map: they
// are not put in the process environment and are never logged. A file
// readable by group or others is refused like the executor does. A missing
// file is fs.ErrNotExist.
func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by group/others (mode %v); run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Credentials reads the demo keys from path. It fails with ErrNotDemo
// unless ETORO_ENV is exactly "demo", and with fs.ErrNotExist when the file
// is missing; the returned config is for etoro.NewClient and nothing else.
func Credentials(path string) (etoro.Config, error) {
	return credentials(path, etoro.EnvDemo)
}

// ErrNotReal: the file is not for the real environment.
var ErrNotReal = errors.New("ETORO_ENV must be real")

// RealCredentials (2026-10-22) reads the REAL account's keys for the
// measurement canary (cmd -etoro-real-canary). It fails with ErrNotReal
// unless ETORO_ENV is exactly "real": the canary process can no more load a
// demo key than the demo process a real one. Only api.NewEtoroRealCanary
// consumes the result, after the acknowledgement check.
func RealCredentials(path string) (etoro.Config, error) {
	return credentials(path, etoro.EnvReal)
}

func credentials(path string, want etoro.Environment) (etoro.Config, error) {
	kv, err := LoadEnvFile(path)
	if err != nil {
		return etoro.Config{}, err
	}
	if env := kv["ETORO_ENV"]; env != string(want) {
		sentinel := ErrNotDemo
		if want == etoro.EnvReal {
			sentinel = ErrNotReal
		}
		return etoro.Config{}, fmt.Errorf("%w (%s has %q)", sentinel, path, env)
	}
	if kv["ETORO_PUBLIC_KEY"] == "" || kv["ETORO_PRIVATE_KEY"] == "" {
		return etoro.Config{}, fmt.Errorf("%s: ETORO_PUBLIC_KEY and ETORO_PRIVATE_KEY are required", path)
	}
	return etoro.Config{PublicKey: kv["ETORO_PUBLIC_KEY"], PrivateKey: kv["ETORO_PRIVATE_KEY"], Env: want}, nil
}

// IsNotExist reports whether err is a missing env file.
func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
