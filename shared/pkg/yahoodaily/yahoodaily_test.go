package yahoodaily

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
)

// Two QQQ sessions (2026-10-08, 2026-10-09; timestamps are the 09:30 ET
// session starts), a null row, and the in-progress 2026-10-12 session.
const sample = `{"chart":{"result":[{"meta":{"symbol":"QQQ","exchangeTimezoneName":"America/New_York"},
"timestamp":[1791466200,1791552600,1791639000,1791811800],
"indicators":{"quote":[{"open":[750.1,747.0,null,752.0],"high":[752.0,752.5,null,753.0],"low":[745.0,746.1,null,751.0],
"close":[747.58,751.27,null,752.5],"volume":[1000,2000,null,10]}],
"adjclose":[{"adjclose":[745.0,748.66,null,752.5]}]}}],"error":null}}`

func TestParse(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC) // Monday 11:00 EDT: session open
	rows, err := Parse([]byte(sample), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Date != "2026-10-08" || rows[1].Date != "2026-10-09" {
		t.Fatalf("rows %+v (null row and unclosed session must be dropped)", rows)
	}
	if rows[1].Close != 751.27 || rows[1].AdjClose != 748.66 || rows[1].Volume != 2000 {
		t.Fatalf("row %+v", rows[1])
	}
	tr := TotalReturnBars(rows)
	k := 748.66 / 751.27
	if math.Abs(tr[1].Open-747.0*k) > 1e-9 || tr[1].Close != 748.66 {
		t.Fatalf("total-return bar %+v", tr[1])
	}
	after, _ := Parse([]byte(sample), time.Date(2026, 10, 12, 20, 30, 0, 0, time.UTC))
	if len(after) != 3 {
		t.Fatalf("after the close the 10-12 bar counts: %d rows", len(after))
	}
}

func TestParseFallsBackForIndexYields(t *testing.T) {
	raw := `{"chart":{"result":[{"meta":{"exchangeTimezoneName":"America/Chicago"},"timestamp":[1791552600],
	"indicators":{"quote":[{"open":[null],"high":[null],"low":[null],"close":[4.057],"volume":[0]}]}}]}}`
	rows, err := Parse([]byte(raw), time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if err != nil || len(rows) != 1 || rows[0].Open != 4.057 || rows[0].AdjClose != 4.057 {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestCSVRoundTripAndResearchReader(t *testing.T) {
	rows, _ := Parse([]byte(sample), time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC))
	p := filepath.Join(t.TempDir(), "qqq.csv")
	if err := WriteCSV(p, "QQQ", rows); err != nil {
		t.Fatal(err)
	}
	back, err := ReadCSV(p)
	if err != nil || len(back) != 2 || back[1] != rows[1] {
		t.Fatalf("round trip %+v %v", back, err)
	}
	br, err := bitsodaily.ReadCSV(p)
	if err != nil || len(br) != 2 || br[1].Close != 751.27 {
		t.Fatalf("research reader: %+v %v", br, err)
	}
}

func TestSeries(t *testing.T) {
	s := NewSeries([]Row{{Date: "2026-10-08", Close: 4.1}, {Date: "2026-10-09", Close: 4.05}}, func(r Row) float64 { return r.Close })
	if v, ok := s.At(time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)); !ok || v != 4.05 {
		t.Fatalf("carry forward: %v %v", v, ok)
	}
	if _, ok := s.At(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("before first value")
	}
	if d, v, ok := s.Last(); !ok || v != 4.05 || d.Format("2006-01-02") != "2026-10-09" {
		t.Fatal("last")
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/%5ENDX" && r.URL.Path != "/^NDX" {
			t.Errorf("path %q", r.URL.Path)
		}
		if r.URL.Query().Get("interval") != "1d" || r.URL.Query().Get("period1") != "0" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL + "/"
	defer func() { BaseURL = old }()
	rows, err := Fetch(context.Background(), srv.Client(), "^NDX", time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC))
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %v", rows, err)
	}
}
