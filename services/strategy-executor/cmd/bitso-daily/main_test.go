package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func ms(s string) int64 {
	t, _ := time.Parse(time.RFC3339, s)
	return t.UnixMilli()
}

// Buckets start at Mexico City midnight: 05:00 UTC while Mexico had DST
// (until Oct 2022), 06:00 UTC after. Both must label as the Mexico date,
// never the previous or next UTC day.
func TestToRows_LabelsByMexicoDate(t *testing.T) {
	cs := []candle{
		{BucketStart: ms("2018-07-01T05:00:00Z"), FirstRate: "1", MaxRate: "2", MinRate: "0.5", LastRate: "1.5"}, // DST
		{BucketStart: ms("2026-09-01T06:00:00Z"), FirstRate: "3", MaxRate: "4", MinRate: "2", LastRate: "3.5"},   // no DST
	}
	rows, dropped := toRows(cs, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if dropped != 0 || len(rows) != 2 {
		t.Fatalf("rows=%d dropped=%d", len(rows), dropped)
	}
	if rows[0].Date != "2018-07-01" || rows[1].Date != "2026-09-01" {
		t.Fatalf("dates %s, %s", rows[0].Date, rows[1].Date)
	}
	r := rows[1]
	if r.Open != 3 || r.High != 4 || r.Low != 2 || r.Close != 3.5 {
		t.Fatalf("ohlc mapped wrong: %+v", r)
	}
}

// Today's bucket has not closed: its last_rate is a live price, not a close.
// Chunk overlaps return the same bucket twice; keep one.
func TestToRows_DropsInProgressAndDuplicates(t *testing.T) {
	now := time.Date(2026, 9, 27, 21, 0, 0, 0, time.UTC)
	c := func(start string) candle {
		return candle{BucketStart: ms(start), FirstRate: "1", MaxRate: "1", MinRate: "1", LastRate: "1"}
	}
	cs := []candle{c("2026-09-26T06:00:00Z"), c("2026-09-26T06:00:00Z"), c("2026-09-27T06:00:00Z")}
	rows, dropped := toRows(cs, now)
	if len(rows) != 1 || rows[0].Date != "2026-09-26" || dropped != 2 {
		t.Fatalf("rows=%+v dropped=%d", rows, dropped)
	}
}

// fetchRange must cover [start, end) in chunks with no hole between them.
func TestFetchRange_ChunksCoverRange(t *testing.T) {
	var spans [][2]int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		e, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
		spans = append(spans, [2]int64{s, e})
		_ = json.NewEncoder(w).Encode(ohlcResponse{Success: true, Payload: []candle{{BucketStart: s, FirstRate: "1", LastRate: "1"}}})
	}))
	defer srv.Close()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(25 * day)
	got, err := fetchRange(srv.Client(), srv.URL, "btc_mxn", start, end, 10*day)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 || len(got) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(spans))
	}
	if spans[0][0] != start.UnixMilli() || spans[2][1] != end.UnixMilli() {
		t.Fatalf("range not covered: %v", spans)
	}
	for i := 1; i < len(spans); i++ {
		if spans[i][0] != spans[i-1][1] {
			t.Fatalf("hole between chunks %d and %d: %v", i-1, i, spans)
		}
	}
}
