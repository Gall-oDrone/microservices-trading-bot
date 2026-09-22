package loader

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ObjectStore is the minimum S3 surface the archive source needs. Narrowing it
// to two methods is what lets the tests run against an in-memory map instead of
// a live bucket or a mocked AWS SDK.
type ObjectStore interface {
	// ListObjects returns every key under prefix, in any order.
	ListObjects(ctx context.Context, prefix string) ([]string, error)
	// GetObject returns the full body of one key.
	GetObject(ctx context.Context, key string) ([]byte, error)
}

// ArchiveConfig configures the S3 archive source.
type ArchiveConfig struct {
	// Bucket is informational for Describe(); the ObjectStore already knows it.
	Bucket string
	// Prefix is the archive root, without a trailing slash. The collector
	// writes under "trades".
	Prefix string
	// Concurrency bounds parallel GetObject calls. The archive is made of
	// thousands of small objects, so throughput here is dominated by request
	// round-trips rather than bytes; a value well above 1 matters a lot.
	Concurrency int
	// SkipFailedObjects keeps the load going when a single object fails to
	// download or decode, recording the count in Stats.ObjectsFailed instead of
	// aborting. Defaults to true: one corrupt 2 KB object should not sink a
	// 34-day load, but the failure must still be visible in the report.
	SkipFailedObjects *bool
}

func (c ArchiveConfig) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return 32
}

func (c ArchiveConfig) skipFailed() bool {
	if c.SkipFailedObjects == nil {
		return true
	}
	return *c.SkipFailedObjects
}

func (c ArchiveConfig) prefix() string {
	p := strings.Trim(c.Prefix, "/")
	if p == "" {
		return "trades"
	}
	return p
}

// S3Archive loads trades from the Parquet archive.
type S3Archive struct {
	store ObjectStore
	cfg   ArchiveConfig
}

// NewS3Archive builds an archive source over any ObjectStore.
func NewS3Archive(store ObjectStore, cfg ArchiveConfig) *S3Archive {
	return &S3Archive{store: store, cfg: cfg}
}

// Describe implements TradeSource.
func (a *S3Archive) Describe() string {
	return fmt.Sprintf("s3://%s/%s (parquet archive)", a.cfg.Bucket, a.cfg.prefix())
}

// DayPrefixes returns the archive prefixes covering [from, to], one per UTC day.
//
// The day partition is derived from each trade's exchange timestamp by the
// collector, so enumerating whole days and then range-filtering rows is both
// correct and the only safe approach: a single object can legitimately straddle
// a day boundary, and the object's own filename timestamp is the flush time,
// not the data time.
func DayPrefixes(root, book string, from, to time.Time) []string {
	if to.Before(from) {
		return nil
	}
	root = strings.Trim(root, "/")
	start := from.UTC().Truncate(24 * time.Hour)
	end := to.UTC().Truncate(24 * time.Hour)

	var out []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, fmt.Sprintf("%s/book=%s/year=%04d/month=%02d/day=%02d/",
			root, book, d.Year(), int(d.Month()), d.Day()))
	}
	return out
}

// LoadTrades implements TradeSource.
//
// Note the deliberate widening of the listing window by one day on each side.
// Because the collector partitions on the first trade of a flush batch, a batch
// that spans midnight lands entirely in the earlier day's prefix. Without the
// pad, trades belonging to `from` could sit under `from-1d` and be missed. The
// extra rows are discarded by Normalize's range filter, so the pad costs
// listing time, not correctness.
func (a *S3Archive) LoadTrades(ctx context.Context, book string, from, to time.Time) ([]ArchiveTrade, Stats, error) {
	st := Stats{Source: a.Describe(), Book: book, From: from, To: to}

	prefixes := DayPrefixes(a.cfg.prefix(), book, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1))
	if len(prefixes) == 0 {
		return nil, st, fmt.Errorf("invalid window: to (%s) is before from (%s)", to, from)
	}

	var keys []string
	for _, p := range prefixes {
		found, err := a.store.ListObjects(ctx, p)
		if err != nil {
			return nil, st, fmt.Errorf("list %s: %w", p, err)
		}
		for _, k := range found {
			if strings.HasSuffix(k, ".parquet") {
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	st.ObjectsListed = len(keys)
	if len(keys) == 0 {
		return nil, st, nil
	}

	type result struct {
		rows []ArchiveTrade
		err  error
	}
	results := make([]result, len(keys))

	sem := make(chan struct{}, a.cfg.concurrency())
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				results[i] = result{err: ctx.Err()}
				return
			}
			body, err := a.store.GetObject(ctx, key)
			if err != nil {
				results[i] = result{err: fmt.Errorf("get %s: %w", key, err)}
				return
			}
			rows, err := DecodeParquetTrades(body)
			if err != nil {
				results[i] = result{err: fmt.Errorf("decode %s: %w", key, err)}
				return
			}
			results[i] = result{rows: rows}
		}(i, key)
	}
	wg.Wait()

	var all []ArchiveTrade
	for _, r := range results {
		if r.err != nil {
			st.ObjectsFailed++
			if !a.cfg.skipFailed() {
				return nil, st, r.err
			}
			continue
		}
		st.ObjectsRead++
		st.RowsDecoded += len(r.rows)
		all = append(all, r.rows...)
	}

	return Normalize(all, book, from, to, &st), st, nil
}

// ---------------------------------------------------------------------------
// Live AWS-backed ObjectStore
// ---------------------------------------------------------------------------

// s3Client is the subset of the AWS SDK used here, kept as an interface so the
// AWS-backed store is itself testable without network access.
type s3Client interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// AWSObjectStore is the production ObjectStore backed by the AWS SDK.
type AWSObjectStore struct {
	client s3Client
	bucket string
}

// NewAWSObjectStore builds an ObjectStore from the ambient AWS config
// (environment, shared credentials file, or instance role).
func NewAWSObjectStore(ctx context.Context, region, bucket string) (*AWSObjectStore, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &AWSObjectStore{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

// NewAWSObjectStoreWithClient injects a client, for tests.
func NewAWSObjectStoreWithClient(client s3Client, bucket string) *AWSObjectStore {
	return &AWSObjectStore{client: client, bucket: bucket}
}

// ListObjects pages through every key under prefix.
func (s *AWSObjectStore) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	var token *string
	for {
		resp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, o := range resp.Contents {
			if o.Key != nil {
				out = append(out, *o.Key)
			}
		}
		if resp.IsTruncated == nil || !*resp.IsTruncated {
			return out, nil
		}
		token = resp.NextContinuationToken
	}
}

// GetObject downloads one key in full.
func (s *AWSObjectStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
