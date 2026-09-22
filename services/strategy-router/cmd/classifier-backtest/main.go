// classifier-backtest replays indicator snapshots through the regime classifier
// and prints a regime distribution histogram (POST-POINT-10 item 9).
//
// Usage:
//   go run ./cmd/classifier-backtest/main.go -url http://127.0.0.1:8084 -book btc_mxn -samples 100
//   cat snapshots.jsonl | go run ./cmd/classifier-backtest/main.go -stdin
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/strategy-router/internal/classifier"
)

// labelledSnapshot pairs a decoded snapshot with the optional SnapshotAt field
// that offline generators (cmd/regime-snapshots in strategy-executor) attach.
// classifier.Snapshot itself has no timestamp, so it is read separately.
type labelledSnapshot struct {
	snap classifier.Snapshot
	at   string
}

func main() {
	url := flag.String("url", "", "strategy-executor base URL (fetches live snapshots)")
	book := flag.String("book", "btc_mxn", "book for live fetch or snapshot label")
	samples := flag.Int("samples", 50, "number of live snapshots when -url is set")
	interval := flag.Duration("interval", 2*time.Second, "pause between live samples")
	stdin := flag.Bool("stdin", false, "read JSONL snapshots from stdin")
	labels := flag.String("labels", "", "also write a `snapshot_at,regime` CSV here (offline attribution)")
	flag.Parse()

	th := classifier.Thresholds{
		ATRHighVolPct: 1.5,
		ATRLowVolPct:  0.30,
		RSIOverbought: 70,
		RSIOversold:   30,
		BBUpper:       0.85,
		BBLower:       0.15,
		EMADistEntry:  0.10,
	}

	var snaps []labelledSnapshot
	if *stdin {
		snaps = readJSONL(os.Stdin)
	} else if *url != "" {
		for _, s := range fetchLive(*url, *book, *samples, *interval) {
			snaps = append(snaps, labelledSnapshot{snap: s})
		}
	} else {
		fmt.Fprintln(os.Stderr, "provide -url or -stdin")
		os.Exit(2)
	}

	var labelFile *os.File
	if *labels != "" {
		f, err := os.Create(*labels)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create labels file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		labelFile = f
		fmt.Fprintln(labelFile, "snapshot_at,regime")
	}

	counts := map[string]int{}
	for _, s := range snaps {
		d := classifier.Classify(s.snap, th)
		counts[d.Regime]++
		if labelFile != nil && s.at != "" {
			fmt.Fprintf(labelFile, "%s,%s\n", s.at, d.Regime)
		}
	}

	fmt.Printf("classifier-backtest: %d snapshots for book=%s\n", len(snaps), *book)
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pct := 100 * float64(counts[k]) / float64(len(snaps))
		fmt.Printf("  %-16s %4d  (%5.1f%%)\n", k, counts[k], pct)
	}
}

func readJSONL(r io.Reader) []labelledSnapshot {
	var out []labelledSnapshot
	sc := bufio.NewScanner(r)
	// Offline snapshot files are line-oriented but can exceed bufio's 64 KiB
	// default if a generator ever widens the shape; raise the cap so a long
	// line is not silently dropped from the distribution.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var s classifier.Snapshot
		if json.Unmarshal(sc.Bytes(), &s) != nil {
			continue
		}
		var meta struct {
			SnapshotAt string `json:"SnapshotAt"`
		}
		_ = json.Unmarshal(sc.Bytes(), &meta)
		out = append(out, labelledSnapshot{snap: s, at: meta.SnapshotAt})
	}
	return out
}

func fetchLive(base, book string, n int, pause time.Duration) []classifier.Snapshot {
	base = strings.TrimRight(base, "/")
	client := &http.Client{Timeout: 10 * time.Second}
	var out []classifier.Snapshot
	for i := 0; i < n; i++ {
		resp, err := client.Get(base + "/api/v1/indicators/" + book + "/snapshot")
		if err == nil && resp.StatusCode == 200 {
			var raw map[string]json.RawMessage
			if json.NewDecoder(resp.Body).Decode(&raw) == nil {
				out = append(out, mapSnapshot(book, raw))
			}
			resp.Body.Close()
		}
		if i+1 < n {
			time.Sleep(pause)
		}
	}
	return out
}

func mapSnapshot(book string, raw map[string]json.RawMessage) classifier.Snapshot {
	s := classifier.Snapshot{Book: book}
	var v struct{ Value float64 }
	if b, ok := raw["atr"]; ok {
		_ = json.Unmarshal(b, &v)
		s.ATR = v.Value
	}
	if b, ok := raw["ema"]; ok {
		_ = json.Unmarshal(b, &v)
		s.EMA = v.Value
	}
	if b, ok := raw["rsi"]; ok {
		_ = json.Unmarshal(b, &v)
		s.RSI = v.Value
	}
	var bb struct {
		Upper        float64 `json:"upper_band"`
		Lower        float64 `json:"lower_band"`
		CurrentPrice float64 `json:"current_price"`
	}
	if b, ok := raw["bollinger"]; ok {
		_ = json.Unmarshal(b, &bb)
		s.BBUpper, s.BBLower = bb.Upper, bb.Lower
		if bb.CurrentPrice > 0 {
			s.Price = bb.CurrentPrice
		}
	}
	if b, ok := raw["sma"]; ok && s.Price == 0 {
		_ = json.Unmarshal(b, &v)
		s.Price = v.Value
	}
	return s
}
