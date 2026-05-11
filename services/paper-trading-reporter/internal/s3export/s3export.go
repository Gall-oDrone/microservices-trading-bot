package s3export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"bitso-trading-platform/paper-trading-reporter/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3PutHeadAPI is the subset of S3 client used here (mock in tests).
type S3PutHeadAPI interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// EnsureBucket creates the bucket if HeadBucket indicates it is missing.
// For us-east-1, LocationConstraint must be omitted. Handles BucketAlreadyOwnedByYou.
func EnsureBucket(ctx context.Context, api S3PutHeadAPI, bucket, region string) error {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return fmt.Errorf("bucket name is empty")
	}

	_, err := api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil
	}
	if !isBucketMissing(err) {
		return fmt.Errorf("head bucket %q: %w", bucket, err)
	}

	in := &s3.CreateBucketInput{Bucket: aws.String(bucket)}
	if region != "" && region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}

	_, err = api.CreateBucket(ctx, in)
	if err == nil {
		return nil
	}
	if isBucketAlreadyOwned(err) {
		return nil
	}
	return fmt.Errorf("create bucket %q: %w", bucket, err)
}

func isBucketMissing(err error) bool {
	if err == nil {
		return false
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NotFound", "NoSuchBucket":
			return true
		}
	}
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == 404 {
		return true
	}
	// Fallback for generic 404-style messages from custom endpoints
	return strings.Contains(strings.ToLower(err.Error()), "notfound") ||
		strings.Contains(strings.ToLower(err.Error()), "statuscode: 404")
}

func isBucketAlreadyOwned(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "BucketAlreadyOwnedByYou", "OperationAborted":
			return true
		}
	}
	return false
}

// ObjectKey builds a stable Hive-style path under prefix.
func ObjectKey(prefix, environment string, collectedAt time.Time, id string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	y, m, d := collectedAt.UTC().Date()
	return fmt.Sprintf("%spaper-trading/%s/%04d/%02d/%02d/snapshot-%s.json",
		prefix, strings.TrimSpace(environment), y, int(m), d, id)
}

// UploadSnapshot marshals the snapshot to indented JSON and PutObject to bucket/key.
func UploadSnapshot(ctx context.Context, api S3PutHeadAPI, bucket, key string, snap *models.PaperTradingSnapshot) (string, error) {
	body, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	_, err = api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("s3://%s/%s", bucket, key), nil
}
