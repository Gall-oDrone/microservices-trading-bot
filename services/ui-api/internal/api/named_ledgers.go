package api

import "strings"

// NamedLedger is one entry of a name=path list.
type NamedLedger struct{ Name, Path string }

// ParseNamedLedgers parses the eToro ledger specs of ui-api and ui-alerts
// (-etoro-ledger): empty -> none; a plain path -> one ledger called
// defaultName; otherwise name=path entries separated by commas (blank or
// malformed entries are skipped). Since 2026-10-21 the index-CFD, ETF and
// dual-momentum cells each keep their own ledger.
func ParseNamedLedgers(spec, defaultName string) []NamedLedger {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if !strings.Contains(spec, "=") {
		return []NamedLedger{{defaultName, spec}}
	}
	var out []NamedLedger
	for _, part := range strings.Split(spec, ",") {
		name, path, ok := strings.Cut(strings.TrimSpace(part), "=")
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)
		if !ok || name == "" || path == "" {
			continue
		}
		out = append(out, NamedLedger{name, path})
	}
	return out
}
