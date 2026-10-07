package alerts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/api"
	"bitso-trading-platform/ui-api/internal/datahealth"
)

func health() api.DataHealthResponse {
	return api.DataHealthResponse{Checks: []api.HealthCheck{
		{ID: "collector.btc_mxn", Area: "collector", Label: "Collector flushes · btc_mxn", Status: datahealth.OK},
		{ID: "compaction.btc_mxn", Area: "archive", Label: "Compaction · btc_mxn", Status: datahealth.Fail, Message: "6 days behind"},
		{ID: "collector.btc_usd", Area: "collector", Label: "Collector flushes · btc_usd", Status: datahealth.Warn, Message: "last flush 100 min ago"},
		{ID: "executor.last_run", Area: "executor", Label: "Last run", Status: datahealth.Unknown, Message: "still running?"},
		{ID: "executor.ledger.btc_mxn", Area: "executor", Label: "Ledger coverage · btc_mxn", Status: datahealth.Fail, Message: "2 closed day(s) not recorded"},
		{ID: "executor.upload", Area: "executor", Label: "Ledger copy in S3", Status: datahealth.Off},
	}}
}

func TestFromHealth(t *testing.T) {
	got := FromHealth("stage", health())
	want := map[string]string{
		"archive/compaction.btc_mxn":    Critical,
		"archive/collector.btc_usd":     Warning,
		"stage/executor.ledger.btc_mxn": Critical,
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, a := range got {
		if want[a.Key] != a.Severity {
			t.Fatalf("%s: %s, want %q", a.Key, a.Severity, want[a.Key])
		}
		if a.Key == "stage/executor.ledger.btc_mxn" && a.Title != "Ledger coverage · btc_mxn (stage)" {
			t.Fatalf("title %q", a.Title)
		}
	}
	// An unreachable archive is a warning (the collector would go unseen); a run
	// in progress is not an alert.
	h := api.DataHealthResponse{Checks: []api.HealthCheck{{ID: "archive", Area: "archive", Label: "Trade archive", Status: datahealth.Unknown}}}
	if a := FromHealth("stage", h); len(a) != 1 || a[0].Severity != Warning || a[0].Key != "archive/archive" {
		t.Fatalf("unreachable archive %+v", a)
	}
}

func TestFromRisk(t *testing.T) {
	r := api.RiskResponse{
		HaltFile: api.HaltFileInfo{Found: true, Error: "halt file x: halted needs reason"},
		Books: []api.BookRisk{
			{Book: "btc_mxn", Findings: []risk.Finding{
				{Rule: api.RuleRunMissed, Severity: risk.Warn, Message: "missing ledger days"},
				{Rule: api.RuleOrderBlocked, Severity: risk.Warn, Message: "1 blocked"},
			}},
			{Book: "btc_usd", Findings: []risk.Finding{{Rule: risk.RuleMaxPositionBTC, Severity: risk.Block, Message: "over"}}},
		},
	}
	got := Merge(FromRisk("stage", r))
	keys := []string{}
	for _, a := range got {
		keys = append(keys, a.Key+"="+a.Severity)
	}
	want := "stage/risk.btc_usd.max_position_btc=critical stage/risk.halt_file=critical stage/risk.btc_mxn.order_blocked=warning"
	if strings.Join(keys, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(keys, " "), want)
	}
}

func TestMergeDedupesArchiveAcrossLedgers(t *testing.T) {
	a := FromHealth("stage", health())
	b := FromHealth("dry-run", health())
	m := Merge(a, b, []Alert{{Key: "archive/collector.btc_usd", Severity: Critical, Title: "x"}})
	n := 0
	for _, x := range m {
		if strings.HasPrefix(x.Key, "archive/") {
			n++
		}
		if x.Key == "archive/collector.btc_usd" && x.Severity != Critical {
			t.Fatalf("worst severity kept: %+v", x)
		}
	}
	if n != 2 || len(m) != 4 {
		t.Fatalf("merged %+v", m)
	}
	for i := 1; i < len(m); i++ {
		if rank(m[i].Severity) > rank(m[i-1].Severity) {
			t.Fatalf("not worst first: %+v", m)
		}
	}
}

func TestDiffLifecycle(t *testing.T) {
	t0 := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) // 06:00 Mexico City
	missed := Alert{Key: "stage/executor.ledger.btc_mxn", Severity: Critical, Title: "Ledger coverage", Message: "1 day"}
	stale := Alert{Key: "archive/collector.btc_usd", Severity: Warning, Title: "Collector", Message: "100 min"}

	// First run: both new.
	n, s := Diff(State{}, []Alert{missed, stale}, t0, 12*time.Hour)
	if len(n.New) != 2 || n.Empty() || n.Open != 2 {
		t.Fatalf("first: %+v", n)
	}
	// 15 min later, unchanged (a new message alone does not re-send): nothing.
	missed.Message = "still 1 day"
	n, s = Diff(s, []Alert{missed, stale}, t0.Add(15*time.Minute), 12*time.Hour)
	if !n.Empty() || s.Open[missed.Key].Message != "still 1 day" || !s.Open[missed.Key].LastSent.Equal(t0) {
		t.Fatalf("quiet: %+v %+v", n, s.Open[missed.Key])
	}
	// The stale collector escalates.
	stale.Severity = Critical
	n, s = Diff(s, []Alert{missed, stale}, t0.Add(30*time.Minute), 12*time.Hour)
	if len(n.Escalated) != 1 || n.Escalated[0].Key != stale.Key || !n.Escalated[0].FirstSeen.Equal(t0) {
		t.Fatalf("escalated: %+v", n)
	}
	// 12 h after it was last sent: a reminder for the missed day only.
	n, s = Diff(s, []Alert{missed, stale}, t0.Add(12*time.Hour), 12*time.Hour)
	if len(n.Reminder) != 1 || n.Reminder[0].Key != missed.Key || len(n.New)+len(n.Escalated) != 0 {
		t.Fatalf("reminder: %+v", n)
	}
	// The collector recovers.
	n, s = Diff(s, []Alert{missed}, t0.Add(12*time.Hour+15*time.Minute), 12*time.Hour)
	if len(n.Resolved) != 1 || n.Resolved[0].Key != stale.Key || n.Open != 1 {
		t.Fatalf("resolved: %+v", n)
	}
	// repeat 0 never reminds.
	if n, _ = Diff(s, []Alert{missed}, t0.Add(100*time.Hour), 0); !n.Empty() {
		t.Fatalf("repeat 0: %+v", n)
	}
	// A severity drop is not news.
	if n, _ = Diff(s, []Alert{{Key: missed.Key, Severity: Warning}}, t0.Add(13*time.Hour), 12*time.Hour); !n.Empty() {
		t.Fatalf("downgrade: %+v", n)
	}
}

func TestRender(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 15, 0, 0, time.UTC)
	n := Notice{At: at, Open: 2,
		New:      []Entry{{Alert: Alert{Key: "a", Severity: Critical, Title: "Ledger coverage · btc_mxn (stage)", Message: "1 closed day(s) not recorded"}, FirstSeen: at}},
		Reminder: []Entry{{Alert: Alert{Key: "b", Severity: Warning, Title: "Compaction · btc_usd", Message: "3 days behind"}, FirstSeen: at.Add(-24 * time.Hour)}},
		Resolved: []Entry{{Alert: Alert{Key: "c", Severity: Warning, Title: "Collector flushes · btc_usd"}}},
	}
	subj, body := Render(n, "http://127.0.0.1:5173/")
	if subj != "[mtb-ops] 1 critical, 1 warning, 1 resolved: Ledger coverage - btc_mxn (stage)" {
		t.Fatalf("subject %q", subj)
	}
	for _, want := range []string{
		"2026-10-08 12:15 UTC (2026-10-08 06:15 Mexico City)",
		"NEW\n  [CRITICAL] Ledger coverage · btc_mxn (stage)\n      1 closed day(s) not recorded\n",
		"STILL OPEN (reminder)\n  [WARNING] Compaction · btc_usd\n      3 days behind\n      open since 2026-10-07 12:15 UTC\n",
		"RESOLVED\n  [OK] Collector flushes · btc_usd",
		"Open alerts now: 2",
		"Data health: http://127.0.0.1:5173/data-health",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
	long := asciiLine(strings.Repeat("é·x", 80))
	if len(long) > 99 || strings.ContainsAny(long, "é·") || !strings.HasSuffix(long, "...") {
		t.Fatalf("subject not SNS-safe: %q", long)
	}
}

type fakeSNS struct {
	in  *sns.PublishInput
	err error
}

func (f *fakeSNS) Publish(_ context.Context, in *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.in = in
	return &sns.PublishOutput{}, f.err
}

func TestSNS(t *testing.T) {
	if _, err := NewSNS(context.Background(), "not-an-arn"); err == nil {
		t.Fatal("bad ARN accepted")
	}
	f := &fakeSNS{}
	s := &SNS{TopicARN: "arn:aws:sns:us-east-1:1:t", client: f}
	if err := s.Notify(context.Background(), "a · b", "body"); err != nil {
		t.Fatal(err)
	}
	if *f.in.Subject != "a - b" || *f.in.Message != "body" || *f.in.TopicArn != s.TopicARN {
		t.Fatalf("publish %+v", f.in)
	}
	f.err = errors.New("AuthorizationError")
	if err := s.Notify(context.Background(), "s", "b"); err == nil || !strings.Contains(err.Error(), "AuthorizationError") {
		t.Fatalf("error %v", err)
	}
}
