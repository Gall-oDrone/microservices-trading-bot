package sink

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client is the subset of S3 API used by S3ObjectSink.
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3ObjectSink uploads objects to an S3 bucket.
type S3ObjectSink struct {
	client S3Client
	bucket string
}

// NewS3ObjectSink creates an S3 sink using default AWS config for the region.
func NewS3ObjectSink(ctx context.Context, region, bucket string) (*S3ObjectSink, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &S3ObjectSink{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
	}, nil
}

// NewS3ObjectSinkWithClient allows injecting a mock client in tests.
func NewS3ObjectSinkWithClient(client S3Client, bucket string) *S3ObjectSink {
	return &S3ObjectSink{client: client, bucket: bucket}
}

func (s *S3ObjectSink) Put(ctx context.Context, key string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", s.bucket, key, err)
	}
	return nil
}
