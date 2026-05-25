package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// MemoStore reads research memos persisted by research-agent under an S3 prefix.
type MemoStore struct {
	client *s3.Client
	bucket string
	prefix string
}

// MemoSummary is a list entry for operator APIs.
type MemoSummary struct {
	RunID       string `json:"run_id"`
	Ticker      string `json:"ticker"`
	TradeDate   string `json:"trade_date"`
	Decision    string `json:"decision"`
	TimestampMs int64  `json:"timestamp_ms"`
	S3ObjectKey string `json:"s3_object_key"`
}

// NewMemoStore creates an S3-backed memo reader. Returns nil if bucket is empty (disabled).
func NewMemoStore(ctx context.Context, bucket, prefix string) (*MemoStore, error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &MemoStore{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// List returns the newest memo summaries up to limit.
func (s *MemoStore) List(ctx context.Context, limit int) ([]MemoSummary, error) {
	if s == nil {
		return nil, fmt.Errorf("research memo store is not configured")
	}
	if limit <= 0 {
		limit = 50
	}

	type item struct {
		key          string
		lastModified time.Time
	}
	var items []item

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".json") {
				continue
			}
			lm := time.Time{}
			if obj.LastModified != nil {
				lm = *obj.LastModified
			}
			items = append(items, item{key: *obj.Key, lastModified: lm})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].lastModified.After(items[j].lastModified)
	})

	out := make([]MemoSummary, 0, limit)
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		memo, err := s.Get(ctx, runIDFromKey(it.key))
		if err != nil {
			continue
		}
		out = append(out, MemoSummary{
			RunID:       stringField(memo, "run_id"),
			Ticker:      stringField(memo, "ticker"),
			TradeDate:   stringField(memo, "trade_date"),
			Decision:    stringField(memo, "decision"),
			TimestampMs: int64Field(memo, "timestamp_ms"),
			S3ObjectKey: it.key,
		})
	}
	return out, nil
}

// Get loads a memo JSON document by run_id.
func (s *MemoStore) Get(ctx context.Context, runID string) (map[string]interface{}, error) {
	if s == nil {
		return nil, fmt.Errorf("research memo store is not configured")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	key := s.prefix + runID + ".json"
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get memo %s: %w", key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	var memo map[string]interface{}
	if err := json.Unmarshal(data, &memo); err != nil {
		return nil, fmt.Errorf("decode memo: %w", err)
	}
	memo["s3_object_key"] = key
	return memo, nil
}

// PutApproval stores an approval audit record next to memos.
func (s *MemoStore) PutApproval(ctx context.Context, auditID string, record map[string]interface{}) error {
	if s == nil {
		return fmt.Errorf("research memo store is not configured")
	}
	auditID = strings.TrimSpace(auditID)
	if auditID == "" {
		return fmt.Errorf("audit_id is required")
	}
	prefix := strings.TrimSuffix(s.prefix, "/")
	approvalPrefix := strings.TrimSuffix(prefix, "/memos") + "/approvals/"
	if !strings.Contains(approvalPrefix, "research/") {
		approvalPrefix = "research/approvals/"
	}
	key := approvalPrefix + auditID + ".json"
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	return err
}

func runIDFromKey(key string) string {
	base := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		base = key[i+1:]
	}
	return strings.TrimSuffix(base, ".json")
}

func stringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func int64Field(m map[string]interface{}, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	default:
		return 0
	}
}
