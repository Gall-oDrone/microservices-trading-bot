package yahoo

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Candidate is one source row plus where it came from. Deduplication picks one
// Candidate per key using the provenance.
type Candidate[T any] struct {
	Row     T
	Key     string    // S3 key of the source object
	ModTime time.Time // S3 LastModified (aws s3 sync preserves it as mtime)
	Kind    SourceKind
	Seq     int // row position within the object (0-based)
}

// SourceKind classifies a source object by layout. For news the order is also
// the preference order when two rows describe the same article.
type SourceKind int

const (
	KindUnknown       SourceKind = iota
	KindJSONL                    // news: daily JSONL copy (no content/summary/metrics columns)
	KindRollupCSV                // news: weekly/monthly/yearly roll-up
	KindDailyCSV                 // news: daily CSV partition (canonical)
	KindPriceLegacy              // prices: <book>/YYYY/MM/DD/
	KindPriceHive                // prices: book=<book>/year=/month=/day=/
	KindPriceSnapshot            // prices: *_full_record.csv multi-book snapshot
)

func (k SourceKind) String() string {
	return [...]string{"unknown", "daily_jsonl", "rollup_csv", "daily_csv", "legacy_layout", "hive_layout", "full_record"}[k]
}

// ReadStats counts what the walker saw before deduplication.
type ReadStats struct {
	Files        int            `json:"files"`
	FilesFailed  int            `json:"files_failed"`
	FailedKeys   []string       `json:"failed_keys,omitempty"`
	Rows         int            `json:"rows"`
	RowsByKind   map[string]int `json:"rows_by_kind"`
	Invalid      int            `json:"invalid_rows"`
	Excluded     int            `json:"excluded_rows"`
	BookMismatch int            `json:"book_path_mismatch,omitempty"`
}

func newReadStats() ReadStats { return ReadStats{RowsByKind: map[string]int{}} }

func (s *ReadStats) fail(key string, err error) {
	s.FilesFailed++
	s.FailedKeys = append(s.FailedKeys, fmt.Sprintf("%s: %v", key, err))
}

// walkFiles visits regular files under dir in lexical order, passing the S3
// key (prefix + relative path, slash-separated) and the file's mtime.
func walkFiles(dir, prefix string, fn func(abs, key string, mod time.Time) error) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		return fn(p, path.Join(prefix, filepath.ToSlash(rel)), info.ModTime().UTC())
	})
}

// readCSV parses a whole CSV file into a header and rows. The header is
// trimmed and stripped of a UTF-8 BOM.
func readCSV(abs string) ([]string, [][]string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	r.FieldsPerRecord = -1
	r.ReuseRecord = false
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for i := range header {
		header[i] = strings.TrimSpace(strings.TrimPrefix(header[i], "\ufeff"))
	}
	var rows [][]string
	for {
		row, err := r.Read()
		if err == io.EOF {
			return header, rows, nil
		}
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
}

func index(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[h] = i
	}
	return m
}

func field(ix map[string]int, row []string, name string) string {
	if i, ok := ix[name]; ok && i < len(row) {
		return strings.TrimSpace(row[i])
	}
	return ""
}

// isNull reports whether a CSV cell is one of the null spellings the
// transform pipeline emits (pandas writes NaN as "nan", Python None as "None").
func isNull(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "nan", "null", "<na>", "nat":
		return true
	}
	return false
}

func strPtr(s string) *string {
	if isNull(s) {
		return nil
	}
	return &s
}

func floatPtr(s string) *float64 {
	if isNull(s) {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func boolPtr(s string) *bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		v := true
		return &v
	case "false", "0", "no":
		v := false
		return &v
	}
	return nil
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

func millisPtr(s string) *int64 {
	if isNull(s) {
		return nil
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, strings.TrimSpace(s)); err == nil {
			ms := t.UTC().UnixMilli()
			return &ms
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Prices
// ---------------------------------------------------------------------------

// priceKind classifies a price key relative to the stocks/crypto/ root and
// returns the book implied by the path ("" for multi-book snapshots).
func priceKind(rel string) (SourceKind, string) {
	segs := strings.Split(rel, "/")
	if len(segs) == 1 {
		return KindPriceSnapshot, ""
	}
	if b, ok := strings.CutPrefix(segs[0], "book="); ok {
		return KindPriceHive, strings.ToLower(b)
	}
	return KindPriceLegacy, strings.ToLower(segs[0])
}

// ReadPrices loads every price CSV under dir (a local `aws s3 sync` copy of
// prefix). Books in exclude are skipped and counted.
func ReadPrices(dir, prefix string, exclude map[string]bool) ([]Candidate[PriceRow], ReadStats, error) {
	st := newReadStats()
	var out []Candidate[PriceRow]
	err := walkFiles(dir, prefix, func(abs, key string, mod time.Time) error {
		if !strings.HasSuffix(key, ".csv") {
			return nil
		}
		st.Files++
		rel := strings.TrimPrefix(strings.TrimPrefix(key, prefix), "/")
		kind, pathBook := priceKind(rel)
		header, rows, err := readCSV(abs)
		if err != nil {
			st.fail(key, err)
			return nil
		}
		ix := index(header)
		for seq, row := range rows {
			st.Rows++
			st.RowsByKind[kind.String()]++
			p, ok := parsePriceRow(ix, row, pathBook)
			if !ok {
				st.Invalid++
				continue
			}
			if pathBook != "" && p.Book != pathBook {
				st.BookMismatch++
			}
			if exclude[p.Book] {
				st.Excluded++
				continue
			}
			p.SourceKey = key
			out = append(out, Candidate[PriceRow]{Row: p, Key: key, ModTime: mod, Kind: kind, Seq: seq})
		}
		return nil
	})
	return out, st, err
}

func parsePriceRow(ix map[string]int, row []string, pathBook string) (PriceRow, bool) {
	book := strings.ToLower(field(ix, row, "book"))
	if book == "" {
		book = pathBook
	}
	// Older files write "2026-03-01"; files written since ~2026-06 write
	// "2026-06-03 00:00:00". Both are UTC calendar days.
	ds := field(ix, row, "date")
	if len(ds) > 10 {
		ds = ds[:10]
	}
	d, err := time.Parse("2006-01-02", ds)
	if book == "" || err != nil {
		return PriceRow{}, false
	}
	p := PriceRow{Book: book, Date: DaysFromDate(d), Ref: field(ix, row, "ref"), CreatedAt: millisPtr(field(ix, row, "created_at"))}
	for _, f := range []struct {
		dst  *float64
		name string
	}{{&p.Open, "open"}, {&p.High, "high"}, {&p.Low, "low"}, {&p.Close, "close"}, {&p.AdjClose, "adj_close"}} {
		v := floatPtr(field(ix, row, f.name))
		if v == nil || *v <= 0 {
			return PriceRow{}, false
		}
		*f.dst = *v
	}
	if v := floatPtr(field(ix, row, "volume")); v != nil && *v >= 0 {
		n := int64(math.Round(*v))
		p.Volume = &n
	}
	return p, true
}

// ---------------------------------------------------------------------------
// News
// ---------------------------------------------------------------------------

func newsKind(key string) SourceKind {
	switch {
	case strings.Contains(key, "/format=jsonl/"):
		return KindJSONL
	case strings.Contains(key, "/day="):
		return KindDailyCSV
	default:
		return KindRollupCSV
	}
}

// CanonicalURL is the news dedup key: scheme and host lower-cased, fragment
// and query removed (on Yahoo the article identity lives in the path; the
// query only carries tracking such as ?pl2=topic-stream_fltrd-strs), and a
// trailing slash trimmed.
func CanonicalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.RawQuery, u.Fragment, u.RawFragment = "", "", ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// ReadNews loads every news CSV and JSONL file under dir (a local copy of
// prefix).
func ReadNews(dir, prefix string) ([]Candidate[NewsRow], ReadStats, error) {
	st := newReadStats()
	var out []Candidate[NewsRow]
	err := walkFiles(dir, prefix, func(abs, key string, mod time.Time) error {
		kind := newsKind(key)
		var rows []NewsRow
		var err error
		switch {
		case strings.HasSuffix(key, ".csv"):
			rows, err = readNewsCSV(abs, &st)
		case strings.HasSuffix(key, ".jsonl"):
			rows, err = readNewsJSONL(abs, &st)
		default:
			return nil
		}
		st.Files++
		if err != nil {
			st.fail(key, err)
			return nil
		}
		for _, r := range rows {
			st.Rows++
			st.RowsByKind[kind.String()]++
			if r.Href == "" {
				st.Invalid++
				continue
			}
			r.SourceKey = key
			out = append(out, Candidate[NewsRow]{Row: r, Key: key, ModTime: mod, Kind: kind})
		}
		return nil
	})
	return out, st, err
}

func readNewsCSV(abs string, st *ReadStats) ([]NewsRow, error) {
	header, rows, err := readCSV(abs)
	if err != nil {
		return nil, err
	}
	ix := index(header)
	out := make([]NewsRow, 0, len(rows))
	for _, row := range rows {
		if len(row) != len(header) {
			st.Rows++
			st.Invalid++
			continue
		}
		g := func(n string) string { return field(ix, row, n) }
		// Text columns are taken untrimmed so article bodies are preserved.
		raw := func(n string) *string {
			if i, ok := ix[n]; ok {
				return strPtr(row[i])
			}
			return nil
		}
		out = append(out, NewsRow{
			Href:                CanonicalURL(g("href")),
			ID:                  g("id"),
			Datetime:            millisPtr(g("datetime")),
			Source:              strPtr(g("source")),
			Headline:            raw("headline"),
			Summary:             raw("summary"),
			Content:             raw("content"),
			Author:              strPtr(g("author")),
			MinsRead:            strPtr(g("minsread")),
			CreatedAt:           millisPtr(g("created_at")),
			LLMFinancialMetrics: strPtr(g("llm_financial_metrics")),
			LLMEntities:         strPtr(g("llm_entities")),
			LLMTicker:           strPtr(g("llm_ticker")),
			LLMEventType:        strPtr(g("llm_event_type")),
			LLMOverallSentiment: floatPtr(g("llm_overall_sentiment")),
			LLMForwardSentiment: floatPtr(g("llm_forward_sentiment")),
			LLMSurpriseScore:    floatPtr(g("llm_surprise_score")),
			LLMRiskScore:        floatPtr(g("llm_risk_score")),
			LLMUncertaintyScore: floatPtr(g("llm_uncertainty_score")),
			LLMImpactStrength:   floatPtr(g("llm_impact_strength")),
			LLMImmediacy:        floatPtr(g("llm_immediacy")),
			LLMImpactHorizon:    strPtr(g("llm_impact_horizon")),
			LLMConfidence:       floatPtr(g("llm_confidence")),
			LLMNoveltyScore:     floatPtr(g("llm_novelty_score")),
			LLMSentimentLabel:   strPtr(g("llm_sentiment_label")),
			LLMImpactLevel:      strPtr(g("llm_impact_level")),
			LLMSignal:           strPtr(g("llm_signal")),
			LLMActionable:       boolPtr(g("llm_actionable")),
			LLMSectors:          strPtr(g("llm_sectors")),
			LLMKeyFacts:         strPtr(g("llm_key_facts")),
			LLMError:            strPtr(g("llm_error")),
		})
	}
	return out, nil
}

// jsonText renders a decoded JSON value in the same shape the CSV carries:
// strings as-is, numbers and bools formatted, arrays/objects as compact JSON.
func jsonText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "True"
		}
		return "False"
	default:
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return ""
		}
		return strings.TrimSpace(b.String())
	}
}

func readNewsJSONL(abs string, st *ReadStats) ([]NewsRow, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var out []NewsRow
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var o map[string]any
		if err := json.Unmarshal(line, &o); err != nil {
			st.Rows++
			st.Invalid++
			continue
		}
		m, _ := o["metadata"].(map[string]any)
		g := func(k string) string {
			if v, ok := o[k]; ok {
				return jsonText(v)
			}
			return jsonText(m[k])
		}
		out = append(out, NewsRow{
			Href:                CanonicalURL(g("url")),
			ID:                  g("id"),
			Datetime:            millisPtr(g("datetime")),
			Source:              strPtr(g("source")),
			Headline:            strPtr(g("title")),
			Summary:             strPtr(g("summary")),
			Content:             strPtr(g("body")),
			Author:              strPtr(g("author")),
			LLMFinancialMetrics: strPtr(g("llm_financial_metrics")),
			LLMEntities:         strPtr(g("llm_entities")),
			LLMTicker:           strPtr(g("llm_ticker")),
			LLMEventType:        strPtr(g("llm_event_type")),
			LLMOverallSentiment: floatPtr(g("llm_overall_sentiment")),
			LLMForwardSentiment: floatPtr(g("llm_forward_sentiment")),
			LLMSurpriseScore:    floatPtr(g("llm_surprise_score")),
			LLMRiskScore:        floatPtr(g("llm_risk_score")),
			LLMUncertaintyScore: floatPtr(g("llm_uncertainty_score")),
			LLMImpactStrength:   floatPtr(g("llm_impact_strength")),
			LLMImmediacy:        floatPtr(g("llm_immediacy")),
			LLMImpactHorizon:    strPtr(g("llm_impact_horizon")),
			LLMConfidence:       floatPtr(g("llm_confidence")),
			LLMNoveltyScore:     floatPtr(g("llm_novelty_score")),
			LLMSentimentLabel:   strPtr(g("llm_sentiment_label")),
			LLMImpactLevel:      strPtr(g("llm_impact_level")),
			LLMSignal:           strPtr(g("llm_signal")),
			LLMActionable:       boolPtr(g("llm_actionable")),
			LLMSectors:          strPtr(g("llm_sectors")),
			LLMKeyFacts:         strPtr(g("llm_key_facts")),
			LLMError:            strPtr(g("llm_error")),
		})
	}
	return out, nil
}

// sortedKeys returns map keys in order, for deterministic reporting.
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
