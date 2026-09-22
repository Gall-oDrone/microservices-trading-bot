// Package archive compacts the collector's small per-flush Parquet objects
// into one validated Parquet file per book/day partition.
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/models"
	"bitso-trading-platform/data-collector/internal/sink"
)

const (
	manifestName    = "_manifest.json"
	manifestVersion = 1
)

var partitionRe = regexp.MustCompile(`^year=(\d{4})/month=(\d{2})/day=(\d{2})$`)

// Options configures a compaction or cutover run.
type Options struct {
	SourcePrefix string // e.g. "trades"
	DestPrefix   string // e.g. "trades_compacted"
	Book         string // e.g. "btc_mxn"
	// Settle is how long after a UTC day ends before it is eligible, so
	// late flushes of that day's rows are not missed.
	Settle  time.Duration
	Workers int
	Force   bool // rebuild even when the manifest says the partition is current
	DryRun  bool // report what would be done without writing
	Now     func() time.Time
	Logger  *log.Logger
}

func (o *Options) defaults() {
	if o.Workers < 1 {
		o.Workers = 16
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	if o.Logger == nil {
		o.Logger = log.Default()
	}
}

// Manifest records what a compacted partition was built from. It is written
// only after the compacted object has been read back and validated.
type Manifest struct {
	Version           int       `json:"version"`
	Book              string    `json:"book"`
	Partition         string    `json:"partition"`
	SourcePrefix      string    `json:"source_prefix"`
	SourceFingerprint string    `json:"source_fingerprint"`
	SourceFiles       int       `json:"source_files"`
	SourceBytes       int64     `json:"source_bytes"`
	SourceRows        int       `json:"source_rows"`
	SourceKeys        []string  `json:"source_keys"`
	CompactedKey      string    `json:"compacted_key"`
	CompactedBytes    int64     `json:"compacted_bytes"`
	CompactedRows     int       `json:"compacted_rows"`
	DuplicateTIDs     int       `json:"duplicate_tids"`
	CreatedAt         time.Time `json:"created_at"`
}

// Status of a partition after a compaction run.
type Status string

const (
	StatusCompacted  Status = "compacted"
	StatusUpToDate   Status = "up-to-date"
	StatusNotSettled Status = "not-settled"
	StatusWouldRun   Status = "would-compact"
	StatusFailed     Status = "FAILED"
)

// PartitionReport is the per-partition before/after comparison.
type PartitionReport struct {
	Partition      string
	Status         Status
	SourceFiles    int
	SourceBytes    int64
	SourceRows     int
	CompactedFiles int
	CompactedBytes int64
	CompactedRows  int
	DuplicateTIDs  int
	Err            error
}

// Summary aggregates a compaction run.
type Summary struct {
	Partitions []PartitionReport
	Ignored    []string // source keys that are not partition Parquet files
}

// Totals sums the before/after numbers over partitions that are compacted or
// already up to date (i.e. those with validated output).
func (s Summary) Totals() (srcFiles int, srcBytes int64, srcRows, dstFiles int, dstBytes int64, dstRows int) {
	for _, p := range s.Partitions {
		if p.Status != StatusCompacted && p.Status != StatusUpToDate {
			continue
		}
		srcFiles += p.SourceFiles
		srcBytes += p.SourceBytes
		srcRows += p.SourceRows
		dstFiles += p.CompactedFiles
		dstBytes += p.CompactedBytes
		dstRows += p.CompactedRows
	}
	return
}

// Failed reports how many partitions failed.
func (s Summary) Failed() int {
	n := 0
	for _, p := range s.Partitions {
		if p.Status == StatusFailed {
			n++
		}
	}
	return n
}

type partition struct {
	name    string // year=YYYY/month=MM/day=DD
	day     time.Time
	objects []Object
}

func bookRoot(prefix, book string) string {
	return fmt.Sprintf("%s/book=%s/", prefix, book)
}

func compactedName(day time.Time) string {
	return "trades-" + day.Format("20060102") + ".parquet"
}

// CutoverName is the file that replaces a partition's small files under the
// source prefix after cutover.
func CutoverName(day time.Time) string {
	return "compacted-" + day.Format("20060102") + ".parquet"
}

// listPartitions groups every .parquet object under prefix/book=<book>/ by
// day partition, returning partitions in chronological order.
func listPartitions(ctx context.Context, st Store, prefix, book string) ([]partition, []string, error) {
	root := bookRoot(prefix, book)
	objs, err := st.List(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]*partition{}
	var ignored []string
	for _, o := range objs {
		rel := strings.TrimPrefix(o.Key, root)
		dir, file := path.Split(rel)
		dir = strings.TrimSuffix(dir, "/")
		m := partitionRe.FindStringSubmatch(dir)
		if m == nil || !strings.HasSuffix(file, ".parquet") {
			ignored = append(ignored, o.Key)
			continue
		}
		p, ok := byName[dir]
		if !ok {
			day, err := time.Parse("2006-01-02", m[1]+"-"+m[2]+"-"+m[3])
			if err != nil {
				ignored = append(ignored, o.Key)
				continue
			}
			p = &partition{name: dir, day: day.UTC()}
			byName[dir] = p
		}
		p.objects = append(p.objects, o)
	}
	out := make([]partition, 0, len(byName))
	for _, p := range byName {
		sort.Slice(p.objects, func(i, j int) bool { return p.objects[i].Key < p.objects[j].Key })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].day.Before(out[j].day) })
	return out, ignored, nil
}

// fingerprint identifies an exact set of source objects (keys and sizes).
func fingerprint(objs []Object) string {
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = fmt.Sprintf("%s\t%d", o.Key, o.Size)
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(h[:])
}

func (o *Options) settled(day time.Time) bool {
	return !o.Now().Before(day.Add(24 * time.Hour).Add(o.Settle))
}

func (o *Options) destDir(p partition) string {
	return bookRoot(o.DestPrefix, o.Book) + p.name
}

func (o *Options) sourceDir(p partition) string {
	return bookRoot(o.SourcePrefix, o.Book) + p.name
}

// Compact rebuilds each settled partition from the source small files into a
// single Parquet object under DestPrefix. Source objects are never modified.
// A partition counts as done only after its output is read back and matches
// the source rows exactly; only then is its manifest written.
func Compact(ctx context.Context, st Store, opts Options) (Summary, error) {
	opts.defaults()
	parts, ignored, err := listPartitions(ctx, st, opts.SourcePrefix, opts.Book)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Ignored: ignored}
	for _, p := range parts {
		rep := compactPartition(ctx, st, &opts, p)
		logReport(opts.Logger, rep)
		sum.Partitions = append(sum.Partitions, rep)
	}
	return sum, nil
}

func compactPartition(ctx context.Context, st Store, opts *Options, p partition) PartitionReport {
	rep := PartitionReport{Partition: p.name, SourceFiles: len(p.objects)}
	for _, o := range p.objects {
		rep.SourceBytes += o.Size
	}
	fail := func(err error) PartitionReport {
		rep.Status = StatusFailed
		rep.Err = err
		return rep
	}

	if !opts.settled(p.day) {
		rep.Status = StatusNotSettled
		return rep
	}
	cutKey := opts.sourceDir(p) + "/" + CutoverName(p.day)
	if len(p.objects) > 1 && containsKey(p.objects, cutKey) {
		return fail(fmt.Errorf("partial cutover detected (%s exists alongside small files); re-run -cutover to finish it", CutoverName(p.day)))
	}

	fp := fingerprint(p.objects)
	if !opts.Force {
		if m, size, ok := currentManifest(ctx, st, opts, p); ok && m.SourceFingerprint == fp && size == m.CompactedBytes {
			rep.Status = StatusUpToDate
			rep.SourceRows = m.SourceRows
			rep.CompactedFiles = 1
			rep.CompactedBytes = m.CompactedBytes
			rep.CompactedRows = m.CompactedRows
			rep.DuplicateTIDs = m.DuplicateTIDs
			return rep
		}
	}

	rows, err := readAll(ctx, st, p.objects, opts.Workers)
	if err != nil {
		return fail(err)
	}
	rep.SourceRows = len(rows)
	rep.DuplicateTIDs = countDuplicateTIDs(rows)
	if len(rows) == 0 {
		return fail(fmt.Errorf("partition has %d files but 0 rows", len(p.objects)))
	}
	sortTrades(rows)

	if opts.DryRun {
		rep.Status = StatusWouldRun
		return rep
	}

	data, err := sink.EncodeParquet(rows)
	if err != nil {
		return fail(err)
	}
	dstKey := opts.destDir(p) + "/" + compactedName(p.day)
	if err := st.Put(ctx, dstKey, data); err != nil {
		return fail(err)
	}

	back, err := st.Get(ctx, dstKey)
	if err != nil {
		return fail(fmt.Errorf("read back compacted object: %w", err))
	}
	got, err := sink.DecodeParquet(back)
	if err != nil {
		return fail(fmt.Errorf("decode compacted object: %w", err))
	}
	if err := sameRows(rows, got); err != nil {
		return fail(fmt.Errorf("validation failed: %w", err))
	}
	rep.CompactedFiles = 1
	rep.CompactedBytes = int64(len(back))
	rep.CompactedRows = len(got)

	keys := make([]string, len(p.objects))
	for i, o := range p.objects {
		keys[i] = o.Key
	}
	m := Manifest{
		Version:           manifestVersion,
		Book:              opts.Book,
		Partition:         p.name,
		SourcePrefix:      opts.SourcePrefix,
		SourceFingerprint: fp,
		SourceFiles:       rep.SourceFiles,
		SourceBytes:       rep.SourceBytes,
		SourceRows:        rep.SourceRows,
		SourceKeys:        keys,
		CompactedKey:      dstKey,
		CompactedBytes:    rep.CompactedBytes,
		CompactedRows:     rep.CompactedRows,
		DuplicateTIDs:     rep.DuplicateTIDs,
		CreatedAt:         opts.Now().UTC(),
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := st.Put(ctx, opts.destDir(p)+"/"+manifestName, body); err != nil {
		return fail(fmt.Errorf("write manifest: %w", err))
	}
	rep.Status = StatusCompacted
	return rep
}

// currentManifest loads the partition's manifest and the current size of its
// compacted object; ok is false if either is missing.
func currentManifest(ctx context.Context, st Store, opts *Options, p partition) (Manifest, int64, bool) {
	dir := opts.destDir(p) + "/"
	objs, err := st.List(ctx, dir)
	if err != nil {
		return Manifest{}, 0, false
	}
	var haveManifest bool
	sizes := map[string]int64{}
	for _, o := range objs {
		sizes[o.Key] = o.Size
		if o.Key == dir+manifestName {
			haveManifest = true
		}
	}
	if !haveManifest {
		return Manifest{}, 0, false
	}
	raw, err := st.Get(ctx, dir+manifestName)
	if err != nil {
		return Manifest{}, 0, false
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.Version != manifestVersion {
		return Manifest{}, 0, false
	}
	size, ok := sizes[m.CompactedKey]
	if !ok {
		return Manifest{}, 0, false
	}
	return m, size, true
}

func readAll(ctx context.Context, st Store, objs []Object, workers int) ([]models.Trade, error) {
	results := make([][]models.Trade, len(objs))
	errs := make([]error, len(objs))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range objs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := st.Get(ctx, objs[i].Key)
			if err != nil {
				errs[i] = err
				return
			}
			rows, err := sink.DecodeParquet(data)
			if err != nil {
				errs[i] = fmt.Errorf("decode %s: %w", objs[i].Key, err)
				return
			}
			results[i] = rows
		}(i)
	}
	wg.Wait()
	var all []models.Trade
	for i := range objs {
		if errs[i] != nil {
			return nil, errs[i]
		}
		all = append(all, results[i]...)
	}
	return all, nil
}

func sortTrades(rows []models.Trade) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if !a.ExchangeTS.Equal(b.ExchangeTS) {
			return a.ExchangeTS.Before(b.ExchangeTS)
		}
		if a.TID != b.TID {
			return a.TID < b.TID
		}
		return a.ReceivedAt.Before(b.ReceivedAt)
	})
}

func countDuplicateTIDs(rows []models.Trade) int {
	seen := make(map[int64]struct{}, len(rows))
	dups := 0
	for _, r := range rows {
		if _, ok := seen[r.TID]; ok {
			dups++
			continue
		}
		seen[r.TID] = struct{}{}
	}
	return dups
}

func sameRows(want, got []models.Trade) error {
	if len(want) != len(got) {
		return fmt.Errorf("row count: source=%d compacted=%d", len(want), len(got))
	}
	for i := range want {
		a, b := want[i], got[i]
		if a.Book != b.Book || a.TID != b.TID || a.Price != b.Price || a.Amount != b.Amount ||
			a.MakerSide != b.MakerSide || !a.ExchangeTS.Equal(b.ExchangeTS) || !a.ReceivedAt.Equal(b.ReceivedAt) {
			return fmt.Errorf("row %d differs: source=%+v compacted=%+v", i, a, b)
		}
	}
	return nil
}

func containsKey(objs []Object, key string) bool {
	for _, o := range objs {
		if o.Key == key {
			return true
		}
	}
	return false
}

func logReport(l *log.Logger, r PartitionReport) {
	msg := fmt.Sprintf("%-26s %-13s source: files=%d rows=%d bytes=%d -> compacted: files=%d rows=%d bytes=%d",
		r.Partition, r.Status, r.SourceFiles, r.SourceRows, r.SourceBytes,
		r.CompactedFiles, r.CompactedRows, r.CompactedBytes)
	if r.DuplicateTIDs > 0 {
		msg += fmt.Sprintf(" duplicate_tids=%d", r.DuplicateTIDs)
	}
	if r.Err != nil {
		msg += " error=" + r.Err.Error()
	}
	l.Println(msg)
}
