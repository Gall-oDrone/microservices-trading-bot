// Package objstore is the narrow, read-only object-store surface ui-api needs
// (list and get), with an S3 implementation and an in-memory one for tests.
// ui-api never writes to the bucket.
package objstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Object is one listed key.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// Store lists and reads objects. Implementations must be safe for concurrent use.
type Store interface {
	// List returns every object under prefix, sorted by key.
	List(ctx context.Context, prefix string) ([]Object, error)
	// Get returns the body of one key (at most maxGetBytes).
	Get(ctx context.Context, key string) ([]byte, error)
	// Describe names the store for display, e.g. "s3://bucket".
	Describe() string
}

// maxGetBytes caps a single download: ui-api only reads manifests, ledgers,
// candle CSVs and run logs, all far below this.
const maxGetBytes = 64 << 20

// ErrNotFound is returned by Get for a missing key.
var ErrNotFound = errors.New("object not found")

// ParseURI splits "s3://bucket/some/prefix" into bucket and prefix (no
// leading or trailing slash).
func ParseURI(uri string) (bucket, prefix string, err error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "s3" || u.Host == "" {
		return "", "", fmt.Errorf("%q: want s3://bucket[/prefix]", uri)
	}
	return u.Host, strings.Trim(u.Path, "/"), nil
}

// S3 reads one bucket with the default AWS credential chain.
type S3 struct {
	Bucket string
	client *s3.Client
}

// NewS3 loads the default AWS config (env, ~/.aws, instance role). The region
// defaults to us-east-1 when none is configured.
func NewS3(ctx context.Context, bucket string) (*S3, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &S3{Bucket: bucket, client: s3.NewFromConfig(cfg)}, nil
}

// Describe implements Store.
func (s *S3) Describe() string { return "s3://" + s.Bucket }

// List implements Store.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.Bucket), Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			obj := Object{Key: aws.ToString(o.Key), ETag: strings.Trim(aws.ToString(o.ETag), `"`)}
			if o.Size != nil {
				obj.Size = *o.Size
			}
			if o.LastModified != nil {
				obj.LastModified = o.LastModified.UTC()
			}
			out = append(out, obj)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Get implements Store.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	res, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		var nsk interface{ ErrorCode() string }
		if errors.As(err, &nsk) && (nsk.ErrorCode() == "NoSuchKey" || nsk.ErrorCode() == "NotFound") {
			return nil, fmt.Errorf("%s: %w", key, ErrNotFound)
		}
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxGetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxGetBytes {
		return nil, fmt.Errorf("%s: larger than %d bytes", key, maxGetBytes)
	}
	return b, nil
}

// Mem is an in-memory Store for tests.
type Mem struct {
	Name string
	mu   sync.Mutex
	objs map[string]memObj
	// Err, when set, is returned by every call (simulates an outage).
	Err error
}

type memObj struct {
	body []byte
	at   time.Time
}

// Put stores body under key with the given modification time.
func (m *Mem) Put(key string, body []byte, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objs == nil {
		m.objs = map[string]memObj{}
	}
	m.objs[key] = memObj{body: bytes.Clone(body), at: at.UTC()}
}

// Delete removes key.
func (m *Mem) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
}

// Describe implements Store.
func (m *Mem) Describe() string {
	if m.Name != "" {
		return m.Name
	}
	return "mem://test"
}

// List implements Store.
func (m *Mem) List(_ context.Context, prefix string) ([]Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	var out []Object
	for k, o := range m.objs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, Object{Key: k, Size: int64(len(o.body)), LastModified: o.at,
				ETag: fmt.Sprintf("%x-%d", len(o.body), o.at.UnixNano())})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Get implements Store.
func (m *Mem) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	o, ok := m.objs[key]
	if !ok {
		return nil, fmt.Errorf("%s: %w", key, ErrNotFound)
	}
	return bytes.Clone(o.body), nil
}
