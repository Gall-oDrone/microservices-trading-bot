package s3export

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/require"

	"bitso-trading-platform/paper-trading-reporter/internal/models"
)

// apiErr implements smithy.APIError for tests.
type apiErr struct {
	code string
	msg  string
}

func (e apiErr) Error() string               { return e.msg }
func (e apiErr) ErrorCode() string            { return e.code }
func (e apiErr) ErrorMessage() string         { return e.msg }
func (e apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

type fakeS3 struct {
	headErr       error
	createCalls   int
	createErr     error
	putCalls      int
	lastPutKey    string
	lastPutBucket string
}

func (f *fakeS3) HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeS3) CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &s3.CreateBucketOutput{}, nil
}

func (f *fakeS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putCalls++
	f.lastPutBucket = aws.ToString(params.Bucket)
	f.lastPutKey = aws.ToString(params.Key)
	return &s3.PutObjectOutput{}, nil
}

func TestEnsureBucket_existsNoCreate(t *testing.T) {
	f := &fakeS3{}
	err := EnsureBucket(context.Background(), f, "microservices-trading-bot", "us-west-2")
	require.NoError(t, err)
	require.Equal(t, 0, f.createCalls)
}

func TestEnsureBucket_createsWhenNotFound(t *testing.T) {
	f := &fakeS3{headErr: apiErr{code: "NotFound", msg: "NotFound"}}
	err := EnsureBucket(context.Background(), f, "microservices-trading-bot", "eu-west-1")
	require.NoError(t, err)
	require.Equal(t, 1, f.createCalls)
}

func TestEnsureBucket_usEast1NoLocationConstraint(t *testing.T) {
	f := &fakeS3{headErr: apiErr{code: "NotFound", msg: "NotFound"}}
	err := EnsureBucket(context.Background(), f, "microservices-trading-bot", "us-east-1")
	require.NoError(t, err)
	require.Equal(t, 1, f.createCalls)
}

func TestEnsureBucket_alreadyOwnedIgnored(t *testing.T) {
	f := &fakeS3{headErr: apiErr{code: "NotFound", msg: "NotFound"}, createErr: apiErr{code: "BucketAlreadyOwnedByYou", msg: "owned"}}
	err := EnsureBucket(context.Background(), f, "microservices-trading-bot", "eu-west-1")
	require.NoError(t, err)
}

func TestEnsureBucket_otherHeadError(t *testing.T) {
	f := &fakeS3{headErr: apiErr{code: "AccessDenied", msg: "denied"}}
	err := EnsureBucket(context.Background(), f, "microservices-trading-bot", "eu-west-1")
	require.Error(t, err)
}

func TestObjectKey(t *testing.T) {
	ts := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	k := ObjectKey("root/", "paper", ts, "abc")
	require.Equal(t, "root/paper-trading/paper/2026/03/05/snapshot-abc.json", k)
}

func TestUploadSnapshot(t *testing.T) {
	f := &fakeS3{}
	snap := models.NewSnapshot("paper", "http://localhost:8084")
	snap.Strategies = []map[string]interface{}{{"name": "x"}}
	uri, err := UploadSnapshot(context.Background(), f, "b", "k/snap.json", snap)
	require.NoError(t, err)
	require.Equal(t, "s3://b/k/snap.json", uri)
	require.Equal(t, 1, f.putCalls)
	require.Equal(t, "b", f.lastPutBucket)
	require.Equal(t, "k/snap.json", f.lastPutKey)
}

func TestIsBucketMissing_plain404Message(t *testing.T) {
	require.True(t, isBucketMissing(errors.New("http response error StatusCode: 404, x")))
}

func TestIsBucketMissing_smithyResponseError404(t *testing.T) {
	wrapped := &smithyhttp.Response{Response: &http.Response{StatusCode: 404}}
	err := &smithyhttp.ResponseError{Response: wrapped, Err: errors.New("not found")}
	require.True(t, isBucketMissing(err))
}
