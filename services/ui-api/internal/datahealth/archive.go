// Package datahealth checks the data the forward tests depend on: the
// collector's trade archive in S3 (raw flushes and daily compaction) and the
// daily-executor's run logs. Everything here is read-only.
//
// docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §4.5.
package datahealth

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/ui-api/internal/objstore"
)

// Status values, ordered from best to worst by Rank.
const (
	OK      = "ok"
	Off     = "off"     // not configured
	Unknown = "unknown" // could not tell
	Warn    = "warn"
	Fail    = "fail"
)

// Rank orders statuses for Worst: ok < off < unknown < warn < fail.
func Rank(s string) int {
	switch s {
	case OK:
		return 0
	case Off:
		return 1
	case Unknown:
		return 2
	case Warn:
		return 3
	case Fail:
		return 4
	}
	return 2
}

// Worst returns the worst of the statuses (OK for none).
func Worst(ss ...string) string {
	w := OK
	for _, s := range ss {
		if Rank(s) > Rank(w) {
			w = s
		}
	}
	return w
}

// Thresholds. The collector flushes one Parquet object per book about every
// hour (time-based), so an hour and a half without a new object is late and
// three hours is an outage. Compaction of day D can only run after D ends
// (UTC), so being one day behind is normal.
const (
	RawWarnAfter       = 90 * time.Minute
	RawFailAfter       = 3 * time.Hour
	FlushGapWarn       = 2 * time.Hour
	CompactionWarnDays = 2
	CompactionFailDays = 4
)

// RawHealth is the newest raw flush for a book (trades/).
type RawHealth struct {
	Status          string  `json:"status"`
	Message         string  `json:"message"`
	LatestPartition string  `json:"latest_partition"` // YYYY-MM-DD (UTC)
	LatestKey       string  `json:"latest_key"`
	LatestAt        string  `json:"latest_at"` // object LastModified, RFC3339
	AgeMinutes      float64 `json:"age_minutes"`
	ObjectsToday    int     `json:"objects_today"`
	BytesToday      int64   `json:"bytes_today"`
	// Flushes in the 24 h before the check and the longest wait between two
	// consecutive ones (including the wait since the last), a proxy for
	// collector outages: the WebSocket gap table lives in Postgres, inside
	// the VPC.
	Flushes24h         int     `json:"flushes_24h"`
	MaxFlushGapMinutes float64 `json:"max_flush_gap_minutes"`
	FlushGaps24h       int     `json:"flush_gaps_24h"` // waits longer than FlushGapWarn
	// Flushes lists the flush times (object LastModified) in that window, oldest first.
	Flushes []string `json:"flushes"`
}

// CompactionHealth is the newest compacted day for a book (trades_compacted/).
type CompactionHealth struct {
	Status          string `json:"status"`
	Message         string `json:"message"`
	LatestPartition string `json:"latest_partition"`
	ExpectedThrough string `json:"expected_through"` // yesterday, UTC
	DaysBehind      int    `json:"days_behind"`
	Partitions      int    `json:"partitions"`
	FirstPartition  string `json:"first_partition"`
	CreatedAt       string `json:"created_at"` // the latest manifest's created_at
	SourceRows      int64  `json:"source_rows"`
	CompactedRows   int64  `json:"compacted_rows"`
	DuplicateTids   int64  `json:"duplicate_tids"`
	OtherDayRows    int64  `json:"other_day_rows"`
	ManifestError   string `json:"manifest_error,omitempty"`
}

// ArchiveBook is one book's archive health.
type ArchiveBook struct {
	Book      string           `json:"book"`
	Status    string           `json:"status"`
	Raw       RawHealth        `json:"raw"`
	Compacted CompactionHealth `json:"compacted"`
}

// Archive is the S3 archive section.
type Archive struct {
	Status    string        `json:"status"`
	Source    string        `json:"source"` // s3://bucket, or "" when off
	CheckedAt string        `json:"checked_at"`
	Error     string        `json:"error,omitempty"`
	Books     []ArchiveBook `json:"books"`
}

type manifest struct {
	CompactedRows int64  `json:"compacted_rows"`
	SourceRows    int64  `json:"source_rows"`
	DuplicateTids int64  `json:"duplicate_tids"`
	OtherDayRows  int64  `json:"other_day_rows"`
	CreatedAt     string `json:"created_at"`
}

var partRe = regexp.MustCompile(`/year=(\d{4})/month=(\d{2})/day=(\d{2})/`)

// partitionDay returns the YYYY-MM-DD of a hive-partitioned key, or "".
func partitionDay(key string) string {
	m := partRe.FindStringSubmatch(key)
	if m == nil {
		return ""
	}
	return m[1] + "-" + m[2] + "-" + m[3]
}

func dayPrefix(root, book string, d time.Time) string {
	return fmt.Sprintf("%s/book=%s/year=%04d/month=%02d/day=%02d/", root, book, d.Year(), int(d.Month()), d.Day())
}

// CheckArchive inspects trades/ and trades_compacted/ for each book. A nil
// store returns an "off" section.
func CheckArchive(ctx context.Context, st objstore.Store, books []string, now time.Time) Archive {
	now = now.UTC()
	a := Archive{Status: Off, CheckedAt: now.Format(time.RFC3339), Books: []ArchiveBook{}}
	if st == nil {
		return a
	}
	a.Source = st.Describe()
	results := make([]ArchiveBook, len(books))
	errs := make([]error, len(books))
	var wg sync.WaitGroup
	for i, b := range books {
		wg.Add(1)
		go func(i int, b string) {
			defer wg.Done()
			results[i], errs[i] = checkBook(ctx, st, b, now)
		}(i, b)
	}
	wg.Wait()
	var statuses []string
	for i, r := range results {
		if errs[i] != nil {
			a.Error = errs[i].Error()
			r = ArchiveBook{Book: books[i], Status: Unknown,
				Raw:       RawHealth{Status: Unknown, Message: "cannot list the archive"},
				Compacted: CompactionHealth{Status: Unknown, Message: "cannot list the archive"}}
		}
		statuses = append(statuses, r.Status)
		a.Books = append(a.Books, r)
	}
	a.Status = Worst(statuses...)
	if len(books) == 0 {
		a.Status = Unknown
	}
	return a
}

func checkBook(ctx context.Context, st objstore.Store, book string, now time.Time) (ArchiveBook, error) {
	raw, err := checkRaw(ctx, st, book, now)
	if err != nil {
		return ArchiveBook{}, err
	}
	comp, err := checkCompaction(ctx, st, book, now)
	if err != nil {
		return ArchiveBook{}, err
	}
	return ArchiveBook{Book: book, Status: Worst(raw.Status, comp.Status), Raw: raw, Compacted: comp}, nil
}

func checkRaw(ctx context.Context, st objstore.Store, book string, now time.Time) (RawHealth, error) {
	today := now.Truncate(24 * time.Hour)
	var objs []objstore.Object
	// Today and yesterday cover the 24 h flush window; if both are empty,
	// walk back up to a week to find the last flush at all.
	for i := 0; i < 8; i++ {
		got, err := st.List(ctx, dayPrefix("trades", book, today.AddDate(0, 0, -i)))
		if err != nil {
			return RawHealth{}, err
		}
		objs = append(objs, got...)
		if i >= 1 && len(objs) > 0 {
			break
		}
	}
	r := RawHealth{Flushes: []string{}}
	if len(objs) == 0 {
		r.Status, r.Message = Fail, "no raw trade objects in the last 8 days"
		return r, nil
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].LastModified.Before(objs[j].LastModified) })
	todayDay := today.Format("2006-01-02")
	for _, o := range objs {
		if partitionDay(o.Key) == todayDay {
			r.ObjectsToday++
			r.BytesToday += o.Size
		}
	}
	last := objs[len(objs)-1]
	r.LatestKey, r.LatestAt = last.Key, last.LastModified.Format(time.RFC3339)
	r.LatestPartition = partitionDay(last.Key)
	age := now.Sub(last.LastModified)
	r.AgeMinutes = round1(age.Minutes())

	// Flush cadence over the last 24 h, counting the wait since the last one.
	from := now.Add(-24 * time.Hour)
	prev := from
	var maxGap time.Duration
	for _, o := range objs {
		if o.LastModified.Before(from) {
			prev = o.LastModified
			continue
		}
		r.Flushes24h++
		r.Flushes = append(r.Flushes, o.LastModified.Format(time.RFC3339))
		if g := o.LastModified.Sub(maxTime(prev, from)); g > maxGap {
			maxGap = g
		}
		if o.LastModified.Sub(prev) > FlushGapWarn {
			r.FlushGaps24h++
		}
		prev = o.LastModified
	}
	if g := now.Sub(maxTime(prev, from)); g > maxGap {
		maxGap = g
	}
	if now.Sub(prev) > FlushGapWarn {
		r.FlushGaps24h++
	}
	r.MaxFlushGapMinutes = round1(maxGap.Minutes())

	switch {
	case age > RawFailAfter:
		r.Status = Fail
		r.Message = fmt.Sprintf("no new trades flushed for %s: the collector may be down", human(age))
	case age > RawWarnAfter:
		r.Status = Warn
		r.Message = fmt.Sprintf("last flush %s ago (normally about hourly)", human(age))
	case r.FlushGaps24h > 0:
		r.Status = Warn
		r.Message = fmt.Sprintf("%d wait(s) over %s between flushes in the last 24 h", r.FlushGaps24h, human(FlushGapWarn))
	default:
		r.Status = OK
		r.Message = fmt.Sprintf("last flush %s ago", human(age))
	}
	return r, nil
}

func checkCompaction(ctx context.Context, st objstore.Store, book string, now time.Time) (CompactionHealth, error) {
	c := CompactionHealth{ExpectedThrough: now.Truncate(24*time.Hour).AddDate(0, 0, -1).Format("2006-01-02")}
	objs, err := st.List(ctx, "trades_compacted/book="+book+"/")
	if err != nil {
		return c, err
	}
	var days []string
	manifests := map[string]string{}
	for _, o := range objs {
		if path.Base(o.Key) != "_manifest.json" {
			continue
		}
		if d := partitionDay(o.Key); d != "" {
			days = append(days, d)
			manifests[d] = o.Key
		}
	}
	if len(days) == 0 {
		c.Status, c.Message = Fail, "nothing compacted yet"
		return c, nil
	}
	sort.Strings(days)
	c.Partitions, c.FirstPartition, c.LatestPartition = len(days), days[0], days[len(days)-1]
	c.DaysBehind = max(0, daysBetween(c.LatestPartition, c.ExpectedThrough))

	if b, err := st.Get(ctx, manifests[c.LatestPartition]); err != nil {
		c.ManifestError = err.Error()
	} else {
		var m manifest
		if err := json.Unmarshal(b, &m); err != nil {
			c.ManifestError = "manifest: " + err.Error()
		} else {
			c.CreatedAt, c.SourceRows, c.CompactedRows = m.CreatedAt, m.SourceRows, m.CompactedRows
			c.DuplicateTids, c.OtherDayRows = m.DuplicateTids, m.OtherDayRows
		}
	}
	since := ""
	if t, err := time.Parse(time.RFC3339Nano, c.CreatedAt); err == nil {
		since = fmt.Sprintf("; last compaction ran %s ago", human(now.Sub(t)))
	}
	switch {
	case c.DaysBehind >= CompactionFailDays:
		c.Status = Fail
		c.Message = fmt.Sprintf("compacted through %s, %d days behind%s: is the compaction job scheduled?", c.LatestPartition, c.DaysBehind, since)
	case c.DaysBehind >= CompactionWarnDays:
		c.Status = Warn
		c.Message = fmt.Sprintf("compacted through %s, %d days behind%s", c.LatestPartition, c.DaysBehind, since)
	case c.ManifestError != "":
		c.Status = Warn
		c.Message = "latest manifest unreadable: " + c.ManifestError
	default:
		c.Status = OK
		c.Message = "compacted through " + c.LatestPartition + since
	}
	return c, nil
}

func daysBetween(a, b string) int {
	ta, err1 := time.Parse("2006-01-02", a)
	tb, err2 := time.Parse("2006-01-02", b)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// human renders a duration as "42 min", "3.5 h" or "6 days".
func human(d time.Duration) string {
	switch {
	case d < 0:
		return "0 min"
	case d < 2*time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", d.Hours()), ".0") + " h"
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
