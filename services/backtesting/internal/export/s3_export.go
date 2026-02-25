package export

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3ExportNotifier writes completion event as a JSON object to S3 (e.g. for archival or downstream ETL)
// Key format: prefix/YYYY/MM/DD/backtest-{id}-summary.json
type S3ExportNotifier struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3ExportNotifier creates an S3 export notifier using default credential chain
func NewS3ExportNotifier(ctx context.Context, bucket, prefix, region string) (*S3ExportNotifier, error) {
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "/")
	if prefix != "" {
		prefix += "/"
	}
	return &S3ExportNotifier{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// Notify writes the event to S3
func (s *S3ExportNotifier) Notify(ctx context.Context, event *BacktestCompletionEvent) error {
	body, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	now := time.Now()
	key := fmt.Sprintf("%s%04d/%02d/%02d/backtest-%s-summary.json",
		s.prefix, now.Year(), now.Month(), now.Day(), event.BacktestID)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(string(body)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	return nil
}

// Name returns the notifier name
func (s *S3ExportNotifier) Name() string {
	return "s3_export"
}
