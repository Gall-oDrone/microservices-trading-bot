package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage implements ResultStorage using AWS S3
type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
	logger logger.Logger
}

// NewS3Storage creates a new S3 storage instance using default credential chain
func NewS3Storage(ctx context.Context, bucket, prefix, region string, log logger.Logger) (*S3Storage, error) {
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg)
	prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "/")
	if prefix != "" {
		prefix += "/"
	}
	return &S3Storage{
		client: client,
		bucket: bucket,
		prefix: prefix,
		logger: log,
	}, nil
}

// key returns the S3 object key for a backtest ID
func (s *S3Storage) key(backtestID string) string {
	return s.prefix + backtestID + ".json"
}

// backtestIDFromKey extracts backtest ID from key (prefix/bt-xxx.json -> bt-xxx)
func (s *S3Storage) backtestIDFromKey(key string) string {
	base := key
	if s.prefix != "" && strings.HasPrefix(key, s.prefix) {
		base = strings.TrimPrefix(key, s.prefix)
	}
	return strings.TrimSuffix(base, ".json")
}

// Save saves a backtest result to S3
func (s *S3Storage) Save(ctx context.Context, result *models.BacktestResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}
	key := s.key(result.BacktestID)
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(string(data)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("s3 put object error: %w", err)
	}
	if s.logger != nil {
		s.logger.Debug("Saved backtest result to S3", map[string]interface{}{
			"backtest_id": result.BacktestID,
			"key":         key,
		})
	}
	return nil
}

// Get retrieves a backtest result from S3
func (s *S3Storage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error) {
	key := s.key(backtestID)
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("backtest not found or s3 get error: %w", err)
	}
	defer out.Body.Close()

	var result models.BacktestResult
	if err := json.NewDecoder(out.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return &result, nil
}

// List lists backtest results with optional filters
func (s *S3Storage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error) {
	if filters == nil {
		filters = NewListFilters()
	}
	filters.Validate()

	var results []*models.BacktestResult
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list objects error: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".json") {
				continue
			}
			backtestID := s.backtestIDFromKey(*obj.Key)
			result, err := s.Get(ctx, backtestID)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("Failed to load result", map[string]interface{}{
						"key":   *obj.Key,
						"error": err,
					})
				}
				continue
			}
			if s.matchesFilters(result, filters) {
				results = append(results, result)
			}
		}
	}

	s.sortResults(results, filters.SortBy, filters.SortOrder)
	start := filters.Offset
	if start > len(results) {
		start = len(results)
	}
	end := start + filters.Limit
	if end > len(results) {
		end = len(results)
	}
	return results[start:end], nil
}

// Delete deletes a backtest result from S3
func (s *S3Storage) Delete(ctx context.Context, backtestID string) error {
	key := s.key(backtestID)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete error: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("Deleted backtest result from S3", map[string]interface{}{
			"backtest_id": backtestID,
		})
	}
	return nil
}

// UpdateStatus updates the status and progress (read-modify-write)
func (s *S3Storage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error {
	result, err := s.Get(ctx, backtestID)
	if err != nil {
		return fmt.Errorf("failed to get result: %w", err)
	}
	result.Status = status
	result.Progress = progress
	return s.Save(ctx, result)
}

// Close closes the storage (no-op for S3; client has no persistent connection)
func (s *S3Storage) Close() error {
	return nil
}

func (s *S3Storage) matchesFilters(result *models.BacktestResult, filters *ListFilters) bool {
	if filters.Status != "" && result.Status != filters.Status {
		return false
	}
	if filters.Strategy != "" && result.Config != nil && result.Config.Strategy != filters.Strategy {
		return false
	}
	if filters.Book != "" && result.Config != nil && result.Config.Book != filters.Book {
		return false
	}
	if filters.StartDate != nil && result.StartedAt.Before(*filters.StartDate) {
		return false
	}
	if filters.EndDate != nil && result.StartedAt.After(*filters.EndDate) {
		return false
	}
	return true
}

func (s *S3Storage) sortResults(results []*models.BacktestResult, sortBy, sortOrder string) {
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false
			switch sortBy {
			case "created_at":
				if sortOrder == "asc" {
					shouldSwap = results[j].StartedAt.After(results[j+1].StartedAt)
				} else {
					shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
				}
			default:
				shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
			}
			if shouldSwap {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

var _ ResultStorage = (*S3Storage)(nil)
