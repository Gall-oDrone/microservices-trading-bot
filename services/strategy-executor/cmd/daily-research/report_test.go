package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wavyBars trends up and down so every rule trades and the random baseline runs.
func wavyBars(n int) []bar {
	bs := make([]bar, n)
	for i := range bs {
		p := 100 + 20*math.Sin(float64(i)/9) + float64(i)/10
		bs[i] = bar{Date: day(i), Open: p - 0.5, High: p + 1, Low: p - 1, Close: p}
	}
	return bs
}

// The JSON must carry exactly the numbers in the text table, so a UI built on
// the JSON can never disagree with the committed evidence files.
func TestRunWindow_ReportMatchesText(t *testing.T) {
	bs := wavyBars(240)
	news := map[string]newsDay{}
	for i := 0; i < len(bs); i += 3 {
		news[day(i).Format("2006-01-02")] = newsDay{N: 1, SentSum: math.Sin(float64(i) / 5)}
	}
	var buf bytes.Buffer
	wr := runWindow(&buf, "IN-SAMPLE", bs, news, day(60), day(239), costModel{buy: 0.0088, sell: 0.0088}, 50, 3, 0, 200, 1)

	if wr.Label != "IN-SAMPLE" || wr.From != "2025-03-02" || wr.Bars != 180 || len(wr.Results) != 4 {
		t.Fatalf("window = %+v", wr)
	}
	text := buf.String()
	for _, r := range wr.Results {
		rnd := ""
		if r.Random != nil {
			rnd = fmt.Sprintf("beats %5.1f%% of %d", r.Random.BeatPct, r.Random.Sims)
		}
		line := fmt.Sprintf("%-18s %10.2f %8d %10.1f %10.2f %10.2f %12.2f   %s\n",
			r.Rule, r.ReturnPct, r.RoundTrips, r.ExposurePct, r.MaxDDPct, r.CostPct, r.VsHoldPP, rnd)
		if !strings.Contains(text, line) {
			t.Errorf("text has no row matching the report:\n%q\ntext:\n%s", line, text)
		}
	}
	if wr.Results[0].Rule != "buy_and_hold" || wr.Results[0].Random != nil || wr.Results[0].VsHoldPP != 0 {
		t.Errorf("buy_and_hold row = %+v", wr.Results[0])
	}
	if wr.Results[1].Random == nil {
		t.Errorf("trend row has no random baseline: %+v", wr.Results[1])
	}
}

func TestRunWindow_TooFewBarsIsNoted(t *testing.T) {
	var buf bytes.Buffer
	wr := runWindow(&buf, "OUT-OF-SAMPLE", flatBars(10, 1, 1), nil, day(30), day(40), costModel{}, 50, 3, 0, 0, 1)
	if wr.Note == "" || wr.Bars != 0 || wr.Results == nil || len(wr.Results) != 0 {
		t.Errorf("window = %+v", wr)
	}
	if !strings.Contains(buf.String(), "not enough bars in window") {
		t.Errorf("text = %q", buf.String())
	}
}

func TestWriteReport_SchemaAndNoPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	rep := Report{Flags: map[string]string{"sma": "50"}, Windows: []WindowReport{{Label: "IN-SAMPLE", Results: []RuleResult{{Rule: "buy_and_hold"}}}}}
	if err := writeReport(path, rep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["schema"] != ReportSchema || got["tool"] != "daily-research" || got["generated_at"] == "" {
		t.Errorf("header = %v", got)
	}
	if _, ok := got["commit"]; ok && got["commit"] == "" {
		t.Errorf("empty commit should be omitted")
	}
	row := got["windows"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	if v, ok := row["random"]; !ok || v != nil {
		t.Errorf("random must be present and null for buy_and_hold, got %v (present %v)", v, ok)
	}
}
