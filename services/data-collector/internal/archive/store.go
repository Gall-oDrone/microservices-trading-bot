package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ErrNotFound is returned by Get when the key does not exist.
var ErrNotFound = errors.New("object not found")

// Object is a listed object key and its size in bytes.
type Object struct {
	Key  string
	Size int64
}

// Store is the object-store surface the compactor needs.
type Store interface {
	List(ctx context.Context, prefix string) ([]Object, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, body []byte) error
	Delete(ctx context.Context, keys []string) error
}

// S3Store implements Store against a single S3 bucket.
type S3Store struct {
	client *s3.Client
	bucket string
}

// NewS3Store uses the default AWS credential chain for the region.
func NewS3Store(ctx context.Context, region, bucket string) (*S3Store, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &S3Store{client: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func (s *S3Store) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", s.bucket, prefix, err)
		}
		for _, o := range page.Contents {
			out = append(out, Object{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size)})
		}
	}
	return out, nil
}

func (s *S3Store) Get(ctx context.Context, key string) ([]byte, error) {
	res, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, fmt.Errorf("get s3://%s/%s: %w", s.bucket, key, ErrNotFound)
		}
		return nil, fmt.Errorf("get s3://%s/%s: %w", s.bucket, key, err)
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

func (s *S3Store) Put(ctx context.Context, key string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", s.bucket, key, err)
	}
	return nil
}

// Delete removes keys in batches of 1000 (the DeleteObjects limit) and fails
// if S3 reports any per-key error.
func (s *S3Store) Delete(ctx context.Context, keys []string) error {
	for start := 0; start < len(keys); start += 1000 {
		end := start + 1000
		if end > len(keys) {
			end = len(keys)
		}
		ids := make([]types.ObjectIdentifier, 0, end-start)
		for _, k := range keys[start:end] {
			ids = append(ids, types.ObjectIdentifier{Key: aws.String(k)})
		}
		res, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("delete objects: %w", err)
		}
		if len(res.Errors) > 0 {
			e := res.Errors[0]
			return fmt.Errorf("delete objects: %d errors, first %s: %s",
				len(res.Errors), aws.ToString(e.Key), aws.ToString(e.Message))
		}
	}
	return nil
}

// MemStore is an in-memory Store for tests.
type MemStore struct {
	mu      sync.Mutex
	Objects map[string][]byte
}

func NewMemStore() *MemStore {
	return &MemStore{Objects: make(map[string][]byte)}
}

func (m *MemStore) List(ctx context.Context, prefix string) ([]Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Object
	for k, v := range m.Objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, Object{Key: k, Size: int64(len(v))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemStore) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.Objects[key]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", key, ErrNotFound)
	}
	return append([]byte(nil), v...), nil
}

func (m *MemStore) Put(ctx context.Context, key string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Objects[key] = append([]byte(nil), body...)
	return nil
}

func (m *MemStore) Delete(ctx context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.Objects, k)
	}
	return nil
}
