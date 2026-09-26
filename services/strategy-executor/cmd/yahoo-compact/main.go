// Command yahoo-compact compacts the Yahoo Finance crypto daily prices and the
// LLM-scored crypto news, stored in S3 as thousands of small partitions, into
// ONE deduplicated ZSTD Parquet file per dataset.
//
// It reads local `aws s3 sync` copies (the same pattern as backtest-archive's
// -archive flag), writes the two files locally, re-reads them to verify, and
// prints a report. Uploading is a separate, explicit step so nothing in S3 is
// touched until the report has been read. Source partitions are never
// modified or deleted.
//
//	aws s3 sync s3://test-financial-stocks-bucket/stocks/crypto/ ./src/prices
//	aws s3 sync s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/ ./src/news
//	go run ./cmd/yahoo-compact -prices-dir ./src/prices -news-dir ./src/news -out-dir ./out
//	aws s3 cp ./out/yahoo_crypto_daily.parquet s3://test-financial-stocks-bucket/stocks/compacted/crypto/
//	aws s3 cp ./out/news_crypto_agentic.parquet s3://test-financial-news-bucket/news/compacted/crypto/agentic=true/
//
// The compacted files deliberately live under a compacted/ prefix, outside
// the transformed/ and per-book trees, so existing `aws s3 sync --include`
// patterns never pick them up alongside the partitions they replace.
//
// Deduplication rules (see internal/yahoo):
//
//   - Prices: key (book, date). All three layouts are read. When versions
//     disagree, the most recently written object wins: a bar fetched before
//     its UTC day closed is partial and is superseded by the later fetch.
//   - News: key = canonical article URL. The `id` column is NOT unique per
//     article and is not used as a key. All CSV layouts (daily, weekly,
//     monthly, yearly) and the JSONL copy are read, because some articles
//     exist only in a roll-up or only in the JSONL copy. The kept row is the
//     LLM-scored one, then the richest layout, then the newest object.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/yahoo"
)

const (
	pricesFile = "yahoo_crypto_daily.parquet"
	newsFile   = "news_crypto_agentic.parquet"
)

type datasetReport struct {
	Dataset   string               `json:"dataset"`
	SourceURI string               `json:"source_uri"`
	Output    string               `json:"output"`
	Bytes     int64                `json:"output_bytes"`
	Read      yahoo.ReadStats      `json:"read"`
	Dedup     yahoo.DedupStats     `json:"dedup"`
	First     string               `json:"first"`
	Last      string               `json:"last"`
	Coverage  []yahoo.BookCoverage `json:"coverage,omitempty"`
	ByYear    map[string]int       `json:"rows_by_year,omitempty"`
	Extra     map[string]any       `json:"extra,omitempty"`
}

func main() {
	pricesDir := flag.String("prices-dir", "", "local sync of the prices prefix")
	newsDir := flag.String("news-dir", "", "local sync of the news prefix")
	pricesURI := flag.String("prices-uri", "s3://test-financial-stocks-bucket/stocks/crypto/", "S3 URI the -prices-dir copy was synced from (recorded as lineage)")
	newsURI := flag.String("news-uri", "s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/", "S3 URI the -news-dir copy was synced from (recorded as lineage)")
	outDir := flag.String("out-dir", ".", "directory for the compacted files")
	exclude := flag.String("exclude-books", "test-book", "comma-separated price books to drop")
	gapsFor := flag.String("show-gaps", "btc-usd", "comma-separated books whose individual gaps are printed")
	reportPath := flag.String("report", "", "also write the full report as JSON here")
	flag.Parse()

	if *pricesDir == "" && *newsDir == "" {
		fail(fmt.Errorf("provide -prices-dir and/or -news-dir"))
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail(err)
	}
	now := time.Now().UTC()
	var reports []datasetReport

	if *pricesDir != "" {
		r, err := compactPrices(*pricesDir, *pricesURI, filepath.Join(*outDir, pricesFile), set(*exclude), now)
		if err != nil {
			fail(fmt.Errorf("prices: %w", err))
		}
		printPrices(r, set(*gapsFor))
		reports = append(reports, r)
	}
	if *newsDir != "" {
		r, err := compactNews(*newsDir, *newsURI, filepath.Join(*outDir, newsFile), now)
		if err != nil {
			fail(fmt.Errorf("news: %w", err))
		}
		printNews(r)
		reports = append(reports, r)
	}
	if *reportPath != "" {
		b, _ := json.MarshalIndent(reports, "", "  ")
		if err := os.WriteFile(*reportPath, b, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("\nwrote report to %s\n", *reportPath)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "yahoo-compact: %v\n", err)
	os.Exit(1)
}

func set(csv string) map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			m[s] = true
		}
	}
	return m
}

func prefixOf(uri string) string {
	s := strings.TrimPrefix(uri, "s3://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return strings.TrimSuffix(s[i+1:], "/")
	}
	return ""
}

func footer(dataset, uri string, now time.Time, rows int, stats any) map[string]string {
	b, _ := json.Marshal(stats)
	return map[string]string{
		"dataset":      dataset,
		"source_uri":   uri,
		"generated_at": now.Format(time.RFC3339),
		"generator":    "strategy-executor/cmd/yahoo-compact",
		"row_count":    fmt.Sprint(rows),
		"stats":        string(b),
	}
}

func compactPrices(dir, uri, out string, exclude map[string]bool, now time.Time) (datasetReport, error) {
	r := datasetReport{Dataset: "yahoo_crypto_daily_prices", SourceURI: uri, Output: out}
	cands, rs, err := yahoo.ReadPrices(dir, prefixOf(uri), exclude)
	if err != nil {
		return r, err
	}
	rows, ds := yahoo.DedupPrices(cands)
	r.Read, r.Dedup = rs, ds
	if len(rows) == 0 {
		return r, fmt.Errorf("no rows")
	}
	r.Coverage = yahoo.Coverage(rows)
	minD, maxD := rows[0].Date, rows[0].Date
	for _, p := range rows {
		minD, maxD = min(minD, p.Date), max(maxD, p.Date)
	}
	r.First, r.Last = yahoo.DateFromDays(minD).Format("2006-01-02"), yahoo.DateFromDays(maxD).Format("2006-01-02")

	if err := yahoo.WritePricesFile(out, rows, footer(r.Dataset, uri, now, len(rows), map[string]any{"read": rs, "dedup": ds})); err != nil {
		return r, err
	}
	// Verify: re-read and check the key really is unique and nothing was lost.
	back, _, err := yahoo.ReadPricesFile(out)
	if err != nil {
		return r, fmt.Errorf("verify: %w", err)
	}
	if len(back) != len(rows) {
		return r, fmt.Errorf("verify: wrote %d rows, read back %d", len(rows), len(back))
	}
	seen := make(map[string]bool, len(back))
	for i, p := range back {
		k := p.Book + "|" + fmt.Sprint(p.Date)
		if seen[k] {
			return r, fmt.Errorf("verify: duplicate key %s", k)
		}
		seen[k] = true
		if p.Close != rows[i].Close || p.Book != rows[i].Book || p.Date != rows[i].Date {
			return r, fmt.Errorf("verify: row %d differs after round trip", i)
		}
	}
	if fi, err := os.Stat(out); err == nil {
		r.Bytes = fi.Size()
	}
	return r, nil
}

func compactNews(dir, uri, out string, now time.Time) (datasetReport, error) {
	r := datasetReport{Dataset: "yahoo_crypto_news_llm_scored", SourceURI: uri, Output: out, ByYear: map[string]int{}}
	cands, rs, err := yahoo.ReadNews(dir, prefixOf(uri))
	if err != nil {
		return r, err
	}
	rows, ds := yahoo.DedupNews(cands)
	r.Read, r.Dedup = rs, ds
	if len(rows) == 0 {
		return r, fmt.Errorf("no rows")
	}
	scored, noTime := 0, 0
	for _, n := range rows {
		if n.Scored() {
			scored++
		}
		if t := n.Time(); t.IsZero() {
			noTime++
		} else {
			r.ByYear[t.Format("2006")]++
		}
	}
	r.First = rows[0].Time().Format(time.RFC3339)
	r.Last = rows[len(rows)-1-noTime].Time().Format(time.RFC3339)
	r.Extra = map[string]any{"scored_rows": scored, "rows_without_datetime": noTime}

	if err := yahoo.WriteNewsFile(out, rows, footer(r.Dataset, uri, now, len(rows), map[string]any{"read": rs, "dedup": ds})); err != nil {
		return r, err
	}
	back, _, err := yahoo.ReadNewsFile(out)
	if err != nil {
		return r, fmt.Errorf("verify: %w", err)
	}
	if len(back) != len(rows) {
		return r, fmt.Errorf("verify: wrote %d rows, read back %d", len(rows), len(back))
	}
	seen := make(map[string]bool, len(back))
	for i, n := range back {
		if seen[n.Href] {
			return r, fmt.Errorf("verify: duplicate url %s", n.Href)
		}
		seen[n.Href] = true
		if n.Href != rows[i].Href || deref(n.Content) != deref(rows[i].Content) {
			return r, fmt.Errorf("verify: row %d differs after round trip", i)
		}
	}
	if fi, err := os.Stat(out); err == nil {
		r.Bytes = fi.Size()
	}
	return r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// countsLine renders a count map as "k=v" pairs, largest first.
func countsLine(m map[string]int) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if m[ks[i]] != m[ks[j]] {
			return m[ks[i]] > m[ks[j]]
		}
		return ks[i] < ks[j]
	})
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

func printRead(r datasetReport) {
	fmt.Printf("%s\n%s\n%s\n", strings.Repeat("=", 100), strings.ToUpper(r.Dataset), strings.Repeat("=", 100))
	fmt.Printf("source      : %s\n", r.SourceURI)
	fmt.Printf("files read  : %d (failed %d)\n", r.Read.Files, r.Read.FilesFailed)
	for _, k := range r.Read.FailedKeys {
		fmt.Printf("  FAILED    : %s\n", k)
	}
	kinds := make([]string, 0, len(r.Read.RowsByKind))
	for k, v := range r.Read.RowsByKind {
		kinds = append(kinds, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(kinds)
	fmt.Printf("rows read   : %d  [%s]\n", r.Read.Rows, strings.Join(kinds, " "))
	fmt.Printf("invalid     : %d   excluded: %d\n", r.Read.Invalid, r.Read.Excluded)
	d := r.Dedup
	fmt.Printf("dedup       : %d candidates -> %d unique  (dropped %d: %d identical, %d superseded by a different version)\n",
		d.Input, d.Unique, d.Dropped, d.Identical, d.Superseded)
	fmt.Printf("conflicts   : %d keys had versions with different values\n", d.ConflictKeys)
	for _, e := range d.Examples {
		fmt.Printf("  e.g.      : %s\n", e)
	}
}

func printPrices(r datasetReport, gapsFor map[string]bool) {
	printRead(r)
	if r.Dedup.ConflictKeys > 0 {
		fmt.Printf("max close disagreement within a key: %.3f%%\n", r.Dedup.MaxCloseDiffPct)
	}
	if r.Read.BookMismatch > 0 {
		fmt.Printf("NOTE        : %d rows whose book column differs from their path (column trusted)\n", r.Read.BookMismatch)
	}
	fmt.Printf("span        : %s .. %s, %d books\n", r.First, r.Last, len(r.Coverage))
	fmt.Printf("output      : %s (%.1f KB)\n\n", r.Output, float64(r.Bytes)/1024)
	fmt.Printf("  %-14s %7s %12s %12s %8s\n", "BOOK", "ROWS", "FIRST", "LAST", "MISSING")
	for _, c := range r.Coverage {
		fmt.Printf("  %-14s %7d %12s %12s %8d\n", c.Book, c.Rows, c.First, c.Last, c.Missing)
		if gapsFor[c.Book] {
			for _, g := range c.Gaps {
				fmt.Printf("  %14s   gap %s\n", "", g)
			}
		}
	}
	fmt.Println()
}

func printNews(r datasetReport) {
	printRead(r)
	fmt.Printf("id as a key : %d ids are shared by >1 article URL; %d URLs appear under >1 id\n",
		r.Dedup.IDsSpanningURLs, r.Dedup.URLsWithManyIDs)
	fmt.Printf("dropped from: %s\n", countsLine(r.Dedup.DroppedByKind))
	fmt.Printf("differing   : %s\n", countsLine(r.Dedup.DifferingFields))
	fmt.Printf("span        : %s .. %s\n", r.First, r.Last)
	years := make([]string, 0, len(r.ByYear))
	for y := range r.ByYear {
		years = append(years, y)
	}
	sort.Strings(years)
	for _, y := range years {
		fmt.Printf("  %s : %d articles\n", y, r.ByYear[y])
	}
	fmt.Printf("scored      : %v of %d; without datetime: %v\n", r.Extra["scored_rows"], r.Dedup.Unique, r.Extra["rows_without_datetime"])
	fmt.Printf("output      : %s (%.1f MB)\n\n", r.Output, float64(r.Bytes)/1e6)
}
