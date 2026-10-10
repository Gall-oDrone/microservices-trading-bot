package guard

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadLimits(t *testing.T) {
	l, err := LoadLimits(env(nil))
	if err != nil || l.MaxDailyLoss != DefaultMaxDailyLoss || l.MaxDrawdownPct != DefaultMaxDrawdownPct {
		t.Fatalf("defaults = %+v, %v", l, err)
	}
	l, err = LoadLimits(env(map[string]string{EnvMaxDailyLoss: " 1500 ", EnvMaxDrawdownPct: "7.5"}))
	if err != nil || l.MaxDailyLoss != 1500 || l.MaxDrawdownPct != 7.5 {
		t.Fatalf("set = %+v, %v", l, err)
	}
	for _, bad := range []map[string]string{
		{EnvMaxDailyLoss: "0"},     // 0 used to mean "disabled": not any more
		{EnvMaxDailyLoss: "-10"},   // negative
		{EnvMaxDailyLoss: "5OO"},   // typo
		{EnvMaxDailyLoss: "NaN"},   // parses, but is no limit
		{EnvMaxDrawdownPct: "0"},   // disabled
		{EnvMaxDrawdownPct: "150"}, // not a percentage
	} {
		if _, err := LoadLimits(env(bad)); err == nil {
			t.Errorf("LoadLimits(%v) accepted", bad)
		}
	}
}

func TestCheckStartup(t *testing.T) {
	stage := "https://stage.bitso.com/api"
	prod := "https://api.bitso.com"
	cases := []struct {
		name    string
		s       Startup
		wantErr string
	}{
		{"dry run, nothing configured", Startup{DryRun: true}, ""},
		{"dry run on production URL", Startup{DryRun: true, BitsoBaseURL: prod}, ""},
		{"live on stage with OM", Startup{OrderManagementURL: "http://order-management:8082", BitsoBaseURL: stage}, ""},
		{"live without OM", Startup{BitsoBaseURL: stage}, EnvOrderManagement},
		{"live on production", Startup{OrderManagementURL: "http://om", BitsoBaseURL: prod}, "limited to Bitso stage"},
		{"live on production, allowed", Startup{OrderManagementURL: "http://om", BitsoBaseURL: prod, AllowProduction: true}, ""},
		{"live, unparsable URL", Startup{OrderManagementURL: "http://om", BitsoBaseURL: "stage.bitso.com"}, "cannot parse"},
		{"live, stage-looking host", Startup{OrderManagementURL: "http://om", BitsoBaseURL: "https://stage.bitso.com.evil.example"}, "limited to Bitso stage"},
		{"etoro live on demo", Startup{OrderManagementURL: "http://om", Broker: "etoro", EtoroEnv: "demo"}, ""},
		{"etoro live, env unset means demo", Startup{OrderManagementURL: "http://om", Broker: "etoro"}, ""},
		{"etoro live on real", Startup{OrderManagementURL: "http://om", Broker: "etoro", EtoroEnv: "real"}, "limited to the eToro demo"},
		{"etoro live on real, allowed", Startup{OrderManagementURL: "http://om", Broker: "etoro", EtoroEnv: "real", AllowProduction: true}, ""},
		{"etoro live without OM", Startup{Broker: "etoro", EtoroEnv: "demo"}, EnvOrderManagement},
		{"etoro dry run on real", Startup{DryRun: true, Broker: "etoro", EtoroEnv: "real"}, ""},
	}
	for _, tc := range cases {
		err := CheckStartup(tc.s)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: err %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestFromEnv(t *testing.T) {
	s := FromEnv(env(map[string]string{EnvOrderManagement: " http://om ", EnvAllowProduction: "true"}), false, "x")
	if s.OrderManagementURL != "http://om" || s.AllowProduction || s.BitsoBaseURL != "x" {
		t.Fatalf("FromEnv = %+v (only \"1\" allows production)", s)
	}
	s = FromEnv(env(map[string]string{"BROKER": " eToro ", "ETORO_ENV": "REAL"}), false, "")
	if s.Broker != "etoro" || s.EtoroEnv != "real" {
		t.Fatalf("FromEnv = %+v, want broker etoro and env real (normalized)", s)
	}
}
