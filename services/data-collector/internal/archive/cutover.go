package archive

import (
	"bytes"
	"context"
	"fmt"

	"bitso-trading-platform/data-collector/internal/sink"
)

// CutoverItem is one partition whose small source files will be replaced by
// its validated compacted object.
type CutoverItem struct {
	Partition   string
	CutoverKey  string // written under the source prefix
	DeleteKeys  []string
	DeleteBytes int64
	Rows        int
	manifest    Manifest
}

// CutoverPlan lists what Cutover will do, plus partitions it will not touch.
type CutoverPlan struct {
	Items   []CutoverItem
	Skipped map[string]string // partition -> reason
}

// PlanCutover selects settled partitions whose manifest still matches the
// exact current set of source files. Anything else is skipped with a reason;
// run Compact first to (re)build those partitions.
func PlanCutover(ctx context.Context, st Store, opts Options) (CutoverPlan, error) {
	opts.defaults()
	parts, _, err := listPartitions(ctx, st, opts.SourcePrefix, opts.Book)
	if err != nil {
		return CutoverPlan{}, err
	}
	plan := CutoverPlan{Skipped: map[string]string{}}
	for _, p := range parts {
		if !opts.settled(p.day) {
			plan.Skipped[p.name] = "not settled"
			continue
		}
		cutKey := opts.sourceDir(p) + "/" + CutoverName(p.day)
		var small []Object
		for _, o := range p.objects {
			if o.Key != cutKey {
				small = append(small, o)
			}
		}
		if len(small) == 0 {
			plan.Skipped[p.name] = "already cut over"
			continue
		}
		m, size, ok := currentManifest(ctx, st, &opts, p)
		if !ok {
			plan.Skipped[p.name] = "no validated compacted output (run compaction first)"
			continue
		}
		if m.SourceFingerprint != fingerprint(small) {
			plan.Skipped[p.name] = "source files changed since compaction (re-run compaction)"
			continue
		}
		if size != m.CompactedBytes {
			plan.Skipped[p.name] = "compacted object size differs from manifest (re-run compaction)"
			continue
		}
		item := CutoverItem{Partition: p.name, CutoverKey: cutKey, Rows: m.CompactedRows, manifest: m}
		for _, o := range small {
			item.DeleteKeys = append(item.DeleteKeys, o.Key)
			item.DeleteBytes += o.Size
		}
		plan.Items = append(plan.Items, item)
	}
	return plan, nil
}

// Cutover executes a plan: for each partition it copies the compacted object
// into the source partition, verifies the copy byte-for-byte and by row
// count, and only then deletes that partition's small files.
func Cutover(ctx context.Context, st Store, opts Options, plan CutoverPlan) error {
	opts.defaults()
	for _, it := range plan.Items {
		data, err := st.Get(ctx, it.manifest.CompactedKey)
		if err != nil {
			return fmt.Errorf("%s: read compacted: %w", it.Partition, err)
		}
		rows, err := decodeCount(data)
		if err != nil || rows != it.manifest.SourceRows {
			return fmt.Errorf("%s: compacted object has %d rows, manifest source_rows=%d (err=%v)",
				it.Partition, rows, it.manifest.SourceRows, err)
		}
		if err := st.Put(ctx, it.CutoverKey, data); err != nil {
			return fmt.Errorf("%s: write cutover object: %w", it.Partition, err)
		}
		back, err := st.Get(ctx, it.CutoverKey)
		if err != nil || !bytes.Equal(back, data) {
			return fmt.Errorf("%s: cutover object verification failed (err=%v); small files left in place", it.Partition, err)
		}
		if err := st.Delete(ctx, it.DeleteKeys); err != nil {
			return fmt.Errorf("%s: delete small files: %w", it.Partition, err)
		}
		opts.Logger.Printf("%-26s cut over: wrote %s (%d rows), deleted %d small files (%d bytes)",
			it.Partition, it.CutoverKey, rows, len(it.DeleteKeys), it.DeleteBytes)
	}
	return nil
}

func decodeCount(data []byte) (int, error) {
	rows, err := sink.DecodeParquet(data)
	return len(rows), err
}
