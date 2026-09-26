package yahoo

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// DedupStats reports what deduplication removed. Dropped == Identical +
// Superseded; the split matters because Superseded rows carried DIFFERENT
// values and a rule chose between them.
type DedupStats struct {
	Input      int `json:"input_rows"`
	Unique     int `json:"unique_rows"`
	Dropped    int `json:"dropped_rows"`
	Identical  int `json:"dropped_identical"`  // same values as the kept row
	Superseded int `json:"dropped_superseded"` // different values; lost to the kept row
	// ConflictKeys counts keys whose candidates disagreed on values.
	ConflictKeys int `json:"conflict_keys"`
	// Examples lists a few conflicts for the report.
	Examples []string `json:"conflict_examples,omitempty"`
	// MaxCloseDiffPct is the largest relative close-price disagreement within
	// a key (prices only).
	MaxCloseDiffPct float64 `json:"max_close_diff_pct,omitempty"`
	// News only: how badly the `id` column fails as a key.
	IDsSpanningURLs int `json:"ids_spanning_multiple_urls,omitempty"`
	URLsWithManyIDs int `json:"urls_with_multiple_ids,omitempty"`
	// News only: which columns differed in superseded rows, and which layouts
	// the dropped rows came from.
	DifferingFields map[string]int `json:"differing_fields,omitempty"`
	DroppedByKind   map[string]int `json:"dropped_by_kind,omitempty"`
}

const maxExamples = 8

// newerThan orders candidates by S3 LastModified, then (within one object) by
// row position, then key, so ties resolve the same way on every run.
func newerThan[T any](a, b Candidate[T]) bool {
	if !a.ModTime.Equal(b.ModTime) {
		return a.ModTime.After(b.ModTime)
	}
	if a.Key == b.Key {
		return a.Seq > b.Seq
	}
	return a.Key > b.Key
}

// ---------------------------------------------------------------------------
// Prices: key (book, date); the most recently written version wins.
// ---------------------------------------------------------------------------

// A bar fetched before its UTC day closed is partial; the same date fetched
// later carries the final values. Two cases occur in the archive:
//
//   - across objects: the newest S3 object is authoritative;
//   - within ONE object: the collector appends intraday snapshots of the same
//     date in write order, so the LAST row is the final bar. Measured on the
//     2026-09-26 archive: all 264 conflicting keys were of this kind, and in
//     263/263 checkable cases the last row's close matched the next day's
//     open while earlier rows did not.

type priceKey struct {
	book string
	date int32
}

func priceValues(p PriceRow) [6]float64 {
	v := -1.0
	if p.Volume != nil {
		v = float64(*p.Volume)
	}
	return [6]float64{p.Open, p.High, p.Low, p.Close, p.AdjClose, v}
}

// DedupPrices keeps one bar per (book, date) and returns rows sorted by book,
// then date.
func DedupPrices(cands []Candidate[PriceRow]) ([]PriceRow, DedupStats) {
	st := DedupStats{Input: len(cands)}
	groups := map[priceKey][]Candidate[PriceRow]{}
	for _, c := range cands {
		k := priceKey{c.Row.Book, c.Row.Date}
		groups[k] = append(groups[k], c)
	}
	out := make([]PriceRow, 0, len(groups))
	for k, g := range groups {
		bi := 0
		for i := 1; i < len(g); i++ {
			if newerThan(g[i], g[bi]) {
				bi = i
			}
		}
		best := g[bi]
		out = append(out, best.Row)
		if len(g) == 1 {
			continue
		}
		bv := priceValues(best.Row)
		conflict := false
		lo, hi := best.Row.Close, best.Row.Close
		for i, c := range g {
			if i == bi {
				continue
			}
			if priceValues(c.Row) == bv {
				st.Identical++
			} else {
				st.Superseded++
				conflict = true
			}
			lo, hi = math.Min(lo, c.Row.Close), math.Max(hi, c.Row.Close)
		}
		if conflict {
			st.ConflictKeys++
			d := (hi - lo) / best.Row.Close * 100
			st.MaxCloseDiffPct = math.Max(st.MaxCloseDiffPct, d)
			if len(st.Examples) < maxExamples {
				st.Examples = append(st.Examples, fmt.Sprintf("%s %s: %d versions, close %.6g..%.6g, kept %s",
					k.book, DateFromDays(k.date).Format("2006-01-02"), len(g), lo, hi, best.Key))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Book != out[j].Book {
			return out[i].Book < out[j].Book
		}
		return out[i].Date < out[j].Date
	})
	st.Unique = len(out)
	st.Dropped = st.Input - st.Unique
	return out, st
}

// ---------------------------------------------------------------------------
// News: key = canonical URL; richest, best-scored row wins.
// ---------------------------------------------------------------------------

// betterNews reports whether a should be kept over b. In order:
//  1. an LLM-scored row beats an unscored one (the score is the payload);
//  2. daily CSV > roll-up CSV > JSONL: the CSVs carry content, summary and
//     financial metrics that the JSONL copy lacks, and the daily partition
//     is the canonical write;
//  3. the most recently written object;
//  4. the lexically greater key (determinism only).
func betterNews(a, b Candidate[NewsRow]) bool {
	if a.Row.Scored() != b.Row.Scored() {
		return a.Row.Scored()
	}
	if a.Kind != b.Kind {
		return a.Kind > b.Kind
	}
	return newerThan(a, b)
}

// newsFields renders a row as a column -> JSON value map, ignoring
// provenance, so two rows can be compared column by column.
func newsFields(r NewsRow) map[string]string {
	r.SourceKey = ""
	b, _ := json.Marshal(r)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

// DedupNews keeps one row per canonical URL and returns rows sorted by
// publication time (unknown times last), then URL.
func DedupNews(cands []Candidate[NewsRow]) ([]NewsRow, DedupStats) {
	st := DedupStats{Input: len(cands), DifferingFields: map[string]int{}, DroppedByKind: map[string]int{}}
	groups := map[string][]Candidate[NewsRow]{}
	idURLs := map[string]map[string]bool{}
	for _, c := range cands {
		groups[c.Row.Href] = append(groups[c.Row.Href], c)
		if c.Row.ID != "" {
			if idURLs[c.Row.ID] == nil {
				idURLs[c.Row.ID] = map[string]bool{}
			}
			idURLs[c.Row.ID][c.Row.Href] = true
		}
	}
	for _, us := range idURLs {
		if len(us) > 1 {
			st.IDsSpanningURLs++
		}
	}

	out := make([]NewsRow, 0, len(groups))
	for _, u := range sortedKeys(groups) {
		g := groups[u]
		best := 0
		for i := 1; i < len(g); i++ {
			if betterNews(g[i], g[best]) {
				best = i
			}
		}
		out = append(out, g[best].Row)

		ids := map[string]bool{}
		for _, c := range g {
			ids[c.Row.ID] = true
		}
		if len(ids) > 1 {
			st.URLsWithManyIDs++
		}
		if len(g) == 1 {
			continue
		}
		bf := newsFields(g[best].Row)
		conflict := false
		for i, c := range g {
			if i == best {
				continue
			}
			st.DroppedByKind[c.Kind.String()]++
			cf := newsFields(c.Row)
			same := true
			for k, v := range bf {
				if cf[k] != v {
					same = false
					st.DifferingFields[k]++
				}
			}
			if same {
				st.Identical++
			} else {
				st.Superseded++
				conflict = true
			}
		}
		if conflict {
			st.ConflictKeys++
			if len(st.Examples) < maxExamples {
				st.Examples = append(st.Examples, fmt.Sprintf("%s: %d versions, kept %s (%s)", u, len(g), g[best].Key, g[best].Kind))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].Time(), out[j].Time()
		if ti.IsZero() != tj.IsZero() {
			return tj.IsZero()
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return out[i].Href < out[j].Href
	})
	st.Unique = len(out)
	st.Dropped = st.Input - st.Unique
	return out, st
}

// ---------------------------------------------------------------------------
// Coverage
// ---------------------------------------------------------------------------

// BookCoverage summarises one book's calendar coverage after deduplication.
type BookCoverage struct {
	Book    string   `json:"book"`
	Rows    int      `json:"rows"`
	First   string   `json:"first"`
	Last    string   `json:"last"`
	Missing int      `json:"missing_days"` // calendar days inside [First, Last] with no bar
	Gaps    []string `json:"gaps,omitempty"`
}

// Coverage computes per-book coverage from rows sorted by (book, date). Crypto
// trades every day, so every calendar day without a bar is a gap.
func Coverage(rows []PriceRow) []BookCoverage {
	var out []BookCoverage
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].Book == rows[i].Book {
			j++
		}
		bc := BookCoverage{Book: rows[i].Book, Rows: j - i,
			First: rows[i].Day().Format("2006-01-02"), Last: rows[j-1].Day().Format("2006-01-02")}
		for k := i + 1; k < j; k++ {
			if gap := int(rows[k].Date - rows[k-1].Date - 1); gap > 0 {
				bc.Missing += gap
				from := rows[k-1].Day().AddDate(0, 0, 1)
				to := rows[k].Day().AddDate(0, 0, -1)
				bc.Gaps = append(bc.Gaps, fmt.Sprintf("%s..%s (%dd)", from.Format("2006-01-02"), to.Format("2006-01-02"), gap))
			}
		}
		out = append(out, bc)
		i = j
	}
	return out
}
