// compact-archive merges the collector's small per-flush Parquet objects into
// one validated file per book/day.
//
// Default mode is non-destructive: it rebuilds each settled day from the
// source small files into -dest-prefix and writes a manifest only after the
// output is read back and matches the source rows exactly.
//
// -cutover replaces the small files under -source-prefix with the validated
// compacted file. It prints the plan and requires typing "yes" before it
// deletes anything.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bitso-trading-platform/data-collector/internal/archive"
)

func main() {
	var (
		bucket  = flag.String("bucket", os.Getenv("S3_BUCKET"), "S3 bucket (default $S3_BUCKET)")
		region  = flag.String("region", envOr("AWS_REGION", "us-east-1"), "AWS region")
		src     = flag.String("source-prefix", envOr("S3_PREFIX", "trades"), "prefix holding the collector's small files")
		dst     = flag.String("dest-prefix", "trades_compacted", "prefix for compacted output")
		book    = flag.String("book", envOr("BITSO_BOOK", "btc_mxn"), "book partition to process")
		settle  = flag.Duration("settle", 2*time.Hour, "wait this long after a UTC day ends before compacting it")
		workers = flag.Int("workers", 32, "concurrent S3 downloads")
		force   = flag.Bool("force", false, "rebuild partitions even if their manifest is current")
		dryRun  = flag.Bool("dry-run", false, "read and report only; write nothing")
		cutover = flag.Bool("cutover", false, "replace validated partitions' small files (asks for confirmation)")
	)
	flag.Parse()
	if *bucket == "" {
		log.Fatal("-bucket (or S3_BUCKET) is required")
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := archive.NewS3Store(ctx, *region, *bucket)
	if err != nil {
		log.Fatal(err)
	}
	opts := archive.Options{
		SourcePrefix: *src,
		DestPrefix:   *dst,
		Book:         *book,
		Settle:       *settle,
		Workers:      *workers,
		Force:        *force,
		DryRun:       *dryRun,
		Logger:       logger,
	}

	if *cutover {
		runCutover(ctx, st, opts, logger)
		return
	}
	runCompact(ctx, st, opts, logger, *bucket)
}

func runCompact(ctx context.Context, st archive.Store, opts archive.Options, logger *log.Logger, bucket string) {
	logger.Printf("compacting s3://%s/%s/book=%s/ -> s3://%s/%s/book=%s/ (dry_run=%v)",
		bucket, opts.SourcePrefix, opts.Book, bucket, opts.DestPrefix, opts.Book, opts.DryRun)
	sum, err := archive.Compact(ctx, st, opts)
	if err != nil {
		log.Fatal(err)
	}
	for _, k := range sum.Ignored {
		logger.Printf("ignored non-partition object: %s", k)
	}

	srcFiles, srcBytes, srcRows, dstFiles, dstBytes, dstRows := sum.Totals()
	counts := map[archive.Status]int{}
	var dupTIDs, otherDay int
	for _, p := range sum.Partitions {
		counts[p.Status]++
		dupTIDs += p.DuplicateTIDs
		otherDay += p.OtherDayRows
	}
	fmt.Println()
	fmt.Println("=== compaction summary (validated partitions) ===")
	fmt.Printf("partitions: %d compacted, %d up-to-date, %d not settled, %d would compact, %d FAILED\n",
		counts[archive.StatusCompacted], counts[archive.StatusUpToDate], counts[archive.StatusNotSettled],
		counts[archive.StatusWouldRun], counts[archive.StatusFailed])
	fmt.Printf("files:  %d -> %d\n", srcFiles, dstFiles)
	fmt.Printf("rows:   %d -> %d (match=%v)\n", srcRows, dstRows, srcRows == dstRows)
	fmt.Printf("bytes:  %d (%.1f MiB) -> %d (%.1f MiB)\n",
		srcBytes, mib(srcBytes), dstBytes, mib(dstBytes))
	if srcFiles > 0 && dstFiles > 0 {
		fmt.Printf("avg:    %.1f rows/file, %.1f KiB/file -> %.1f rows/file, %.1f KiB/file\n",
			float64(srcRows)/float64(srcFiles), float64(srcBytes)/1024/float64(srcFiles),
			float64(dstRows)/float64(dstFiles), float64(dstBytes)/1024/float64(dstFiles))
	}
	fmt.Printf("info:   %d duplicate TIDs, %d rows filed under an adjacent day (kept as-is)\n", dupTIDs, otherDay)

	if sum.Failed() > 0 || srcRows != dstRows {
		fmt.Println("RESULT: FAILED — do not cut over; see per-partition errors above")
		os.Exit(1)
	}
	if opts.DryRun {
		fmt.Println("RESULT: dry run only, nothing written")
		return
	}
	fmt.Println("RESULT: OK — original small files untouched. Review, then run with -cutover to replace them.")
}

func runCutover(ctx context.Context, st archive.Store, opts archive.Options, logger *log.Logger) {
	plan, err := archive.PlanCutover(ctx, st, opts)
	if err != nil {
		log.Fatal(err)
	}
	var files, rows int
	var bytes int64
	for _, it := range plan.Items {
		logger.Printf("%-26s replace %d small files (%d bytes) with %s (%d rows)",
			it.Partition, len(it.DeleteKeys), it.DeleteBytes, it.CutoverKey, it.Rows)
		files += len(it.DeleteKeys)
		bytes += it.DeleteBytes
		rows += it.Rows
	}
	for p, reason := range plan.Skipped {
		logger.Printf("%-26s skip: %s", p, reason)
	}
	if len(plan.Items) == 0 {
		fmt.Println("nothing to cut over")
		return
	}

	fmt.Printf("\nCutover will DELETE %d small files (%.1f MiB, %d rows) across %d partitions,\n",
		files, mib(bytes), rows, len(plan.Items))
	fmt.Println("after writing and verifying one compacted file per partition under the source prefix.")
	if opts.DryRun {
		fmt.Println("dry run: nothing changed")
		return
	}
	fmt.Print(`Type "yes" to proceed: `)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(answer) != "yes" {
		fmt.Println("aborted, nothing changed")
		return
	}
	if err := archive.Cutover(ctx, st, opts, plan); err != nil {
		log.Fatalf("cutover stopped: %v", err)
	}
	fmt.Println("cutover complete")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }
