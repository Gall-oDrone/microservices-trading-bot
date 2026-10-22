package etoro

// The real-account acknowledgement (docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
// §5.2, built 2026-10-22). A process may build a client for the REAL
// environment only when all six conditions below hold; every refusal names
// the missing one, so an operator reading the start log knows what to fix
// and nothing ever degrades into a silent read-only start.
//
//  1. the -etoro-real-canary flag was given on the command line (visible
//     in ps; never an environment variable);
//  2. the env file says ETORO_ENV=real and the public key's prefix is the
//     one listed in the ack file (a demo key cannot be promoted by renaming
//     a file);
//  3. the ack file exists, is 0600, carries the exact sentence, the key
//     prefix, the ledger path, the policy version, a start and an end date
//     and a name, and today is inside [start, end];
//  4. the ledger path is under a directory named real-canary, is the one
//     the ack names, and is none of the demo ledgers;
//  5. the policy is the canary policy (its version string, not its shape);
//  6. the operator token file is not the demo one (paths differ and, when
//     both are readable, so do their hashes).
//
// Nothing here reads or prints a key: the caller passes the public key and
// only its prefix is compared; Facts() describes the check without it.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RealAckSentence is the first line of the ack file, verbatim.
const RealAckSentence = "I accept orders on the eToro REAL account for the measurement canary only"

// RealAckPolicyPrefix is what a canary policy version must start with.
const RealAckPolicyPrefix = "etoro-real-canary-"

// Bounds on the key prefix the ack file lists: long enough to identify a
// key, short enough that nobody pastes the key itself into the file.
const (
	realAckPrefixMin = 8
	realAckPrefixMax = 16
)

// Each of the six conditions has its own error so a test (and a log line)
// can tell them apart. Wrapped messages add the specifics.
var (
	ErrRealAckFlag      = errors.New("real canary: -etoro-real-canary was not given on the command line")
	ErrRealAckEnv       = errors.New("real canary: the env file must say ETORO_ENV=real")
	ErrRealAckKeyPrefix = errors.New("real canary: the public key's prefix is not the one the ack file lists")
	ErrRealAckFile      = errors.New("real canary: the acknowledgement file is missing or malformed")
	ErrRealAckWindow    = errors.New("real canary: today is outside the acknowledgement's start/end window")
	ErrRealAckLedger    = errors.New("real canary: the ledger path is not the canary's")
	ErrRealAckPolicy    = errors.New("real canary: the policy is not the canary policy")
	ErrRealAckToken     = errors.New("real canary: the operator token must differ from the demo one")
)

// RealAck is the parsed acknowledgement file.
type RealAck struct {
	Path string `json:"path"`
	// KeyPrefix is the first characters of ETORO_PUBLIC_KEY (the x-api-key)
	// of the canary's key set. Never the key.
	KeyPrefix     string    `json:"key_prefix"`
	LedgerPath    string    `json:"ledger_path"`
	PolicyVersion string    `json:"policy_version"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	Name          string    `json:"name"`
}

// LoadRealAck reads and validates the ack file's shape (condition 3 except
// the date window, which needs the clock: see CheckRealAck). Format: the
// sentence on the first non-comment line, then "key: value" lines
// key_prefix, ledger, policy, start, end, name (dates YYYY-MM-DD).
func LoadRealAck(path string) (RealAck, error) {
	ack := RealAck{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return ack, fmt.Errorf("%w: %v", ErrRealAckFile, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ack, fmt.Errorf("%w: %v", ErrRealAckFile, err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return ack, fmt.Errorf("%w: %s is readable by group/others (mode %v); run: chmod 600 %s", ErrRealAckFile, path, fi.Mode().Perm(), path)
	}
	sentenceSeen := false
	fields := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !sentenceSeen {
			if line != RealAckSentence {
				return ack, fmt.Errorf("%w: line %d must be the sentence %q, verbatim", ErrRealAckFile, n, RealAckSentence)
			}
			sentenceSeen = true
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return ack, fmt.Errorf("%w: line %d is not \"key: value\"", ErrRealAckFile, n)
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if _, dup := fields[k]; dup {
			return ack, fmt.Errorf("%w: %s is given twice", ErrRealAckFile, k)
		}
		fields[k] = v
	}
	if err := sc.Err(); err != nil {
		return ack, fmt.Errorf("%w: %v", ErrRealAckFile, err)
	}
	if !sentenceSeen {
		return ack, fmt.Errorf("%w: %s has no sentence", ErrRealAckFile, path)
	}
	for _, k := range []string{"key_prefix", "ledger", "policy", "start", "end", "name"} {
		if fields[k] == "" {
			return ack, fmt.Errorf("%w: %s is missing", ErrRealAckFile, k)
		}
	}
	for k := range fields {
		switch k {
		case "key_prefix", "ledger", "policy", "start", "end", "name":
		default:
			return ack, fmt.Errorf("%w: unknown field %q", ErrRealAckFile, k)
		}
	}
	ack.KeyPrefix = fields["key_prefix"]
	if n := len(ack.KeyPrefix); n < realAckPrefixMin || n > realAckPrefixMax {
		return ack, fmt.Errorf("%w: key_prefix must be %d to %d characters (a prefix, never the key)", ErrRealAckFile, realAckPrefixMin, realAckPrefixMax)
	}
	ack.LedgerPath = fields["ledger"]
	if !filepath.IsAbs(ack.LedgerPath) {
		return ack, fmt.Errorf("%w: ledger must be an absolute path", ErrRealAckFile)
	}
	ack.PolicyVersion = fields["policy"]
	if !strings.HasPrefix(ack.PolicyVersion, RealAckPolicyPrefix) {
		return ack, fmt.Errorf("%w: policy must start with %q", ErrRealAckFile, RealAckPolicyPrefix)
	}
	if ack.Start, err = time.Parse("2006-01-02", fields["start"]); err != nil {
		return ack, fmt.Errorf("%w: start: %v", ErrRealAckFile, err)
	}
	if ack.End, err = time.Parse("2006-01-02", fields["end"]); err != nil {
		return ack, fmt.Errorf("%w: end: %v", ErrRealAckFile, err)
	}
	if !ack.End.After(ack.Start) {
		return ack, fmt.Errorf("%w: end %s must be after start %s", ErrRealAckFile, fields["end"], fields["start"])
	}
	ack.Name = fields["name"]
	return ack, nil
}

// RealAckCheck is what CheckRealAck compares the ack against: the process's
// own facts. PublicKey is compared by prefix only and never stored.
type RealAckCheck struct {
	// Flag: -etoro-real-canary was on the command line.
	Flag bool
	// Env is the env file's ETORO_ENV; PublicKey its ETORO_PUBLIC_KEY.
	Env       Environment
	PublicKey string
	// LedgerPath is the ledger this process would write; OtherLedgers every
	// other ledger it knows (the demo cells), none of which may be it.
	LedgerPath   string
	OtherLedgers []string
	// PolicyVersion is the version of the policy the trades are checked with.
	PolicyVersion string
	// TokenFile / DemoTokenFile are the operator token paths; TokenSHA256 /
	// DemoTokenSHA256 their hex digests (DemoTokenSHA256 empty when the demo
	// token could not be read: the hashes are then not compared, the paths are).
	TokenFile       string
	DemoTokenFile   string
	TokenSHA256     string
	DemoTokenSHA256 string
	// Now is the clock the date window is checked against (its date in
	// New York decides; the caller passes a time in that zone or UTC).
	Now time.Time
}

// CheckRealAck verifies the six conditions in order and returns the first
// one that fails, wrapped in its sentinel. nil means a real client may be
// built.
func CheckRealAck(ack RealAck, in RealAckCheck) error {
	// 1. The flag.
	if !in.Flag {
		return ErrRealAckFlag
	}
	// 2. The environment and the key prefix.
	if in.Env != EnvReal {
		return fmt.Errorf("%w (got %q)", ErrRealAckEnv, in.Env)
	}
	if strings.TrimSpace(in.PublicKey) == "" || !strings.HasPrefix(in.PublicKey, ack.KeyPrefix) {
		return fmt.Errorf("%w (prefix of %d characters)", ErrRealAckKeyPrefix, len(ack.KeyPrefix))
	}
	// 3. The window (the file's shape was checked by LoadRealAck).
	day := time.Date(in.Now.Year(), in.Now.Month(), in.Now.Day(), 0, 0, 0, 0, time.UTC)
	if day.Before(ack.Start) || day.After(ack.End) {
		return fmt.Errorf("%w (%s not in %s..%s)", ErrRealAckWindow, day.Format("2006-01-02"), ack.Start.Format("2006-01-02"), ack.End.Format("2006-01-02"))
	}
	// 4. The ledger.
	ledger := filepath.Clean(in.LedgerPath)
	if !filepath.IsAbs(ledger) {
		if abs, err := filepath.Abs(ledger); err == nil {
			ledger = abs
		}
	}
	if filepath.Base(filepath.Dir(ledger)) != "real-canary" {
		return fmt.Errorf("%w: %s is not under a directory named real-canary", ErrRealAckLedger, ledger)
	}
	for _, el := range strings.Split(filepath.ToSlash(ledger), "/") {
		if strings.HasPrefix(strings.ToLower(el), "demo") {
			return fmt.Errorf("%w: %s has a demo path element (%s)", ErrRealAckLedger, ledger, el)
		}
	}
	if ack.LedgerPath != "" && filepath.Clean(ack.LedgerPath) != ledger {
		return fmt.Errorf("%w: the ack names %s, the process would write %s", ErrRealAckLedger, ack.LedgerPath, ledger)
	}
	for _, o := range in.OtherLedgers {
		if filepath.Clean(o) == ledger {
			return fmt.Errorf("%w: %s is also a demo ledger of this process", ErrRealAckLedger, ledger)
		}
	}
	// 5. The policy.
	if !strings.HasPrefix(in.PolicyVersion, RealAckPolicyPrefix) || in.PolicyVersion != ack.PolicyVersion {
		return fmt.Errorf("%w: the process has %q, the ack names %q", ErrRealAckPolicy, in.PolicyVersion, ack.PolicyVersion)
	}
	// 6. The token.
	if in.TokenFile == "" {
		return fmt.Errorf("%w: no operator token file", ErrRealAckToken)
	}
	if in.DemoTokenFile != "" && filepath.Clean(in.TokenFile) == filepath.Clean(in.DemoTokenFile) {
		return fmt.Errorf("%w: both are %s", ErrRealAckToken, in.TokenFile)
	}
	if in.DemoTokenSHA256 != "" && in.TokenSHA256 != "" && in.DemoTokenSHA256 == in.TokenSHA256 {
		return fmt.Errorf("%w: %s and %s hold the same token", ErrRealAckToken, in.TokenFile, in.DemoTokenFile)
	}
	return nil
}

// Facts is the one-line summary a start log prints after a successful
// check: the six facts, no secret (the key prefix is described by its
// length only).
func (ack RealAck) Facts(in RealAckCheck) string {
	tokens := "token file differs from the demo one (paths)"
	if in.DemoTokenSHA256 != "" {
		tokens = "token file differs from the demo one (paths and hashes)"
	}
	return fmt.Sprintf("REAL CANARY ACK OK: flag -etoro-real-canary; ETORO_ENV=real, key prefix matched (%d chars); ack %s by %s, %s..%s; ledger %s; policy %s; %s",
		len(ack.KeyPrefix), ack.Path, ack.Name, ack.Start.Format("2006-01-02"), ack.End.Format("2006-01-02"), in.LedgerPath, in.PolicyVersion, tokens)
}
