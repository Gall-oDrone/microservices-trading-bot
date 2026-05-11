// Command paper-trading-reporter collects strategy-executor state (paper / organic runs)
// and uploads a JSON snapshot to S3. Optional: ensure the bucket exists (HeadBucket + CreateBucket).
//
// Example:
//
//	AWS_REGION=us-east-1 go run ./cmd/paper-trading-reporter \
//	  -strategy-executor-url http://127.0.0.1:8084 \
//	  -bucket microservices-trading-bot \
//	  -environment paper
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"bitso-trading-platform/paper-trading-reporter/internal/collector"
	"bitso-trading-platform/paper-trading-reporter/internal/s3export"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func main() {
	os.Exit(run())
}

func run() int {
	baseURL := flag.String("strategy-executor-url", getenv("STRATEGY_EXECUTOR_URL", "http://127.0.0.1:8084"), "strategy-executor base URL")
	bucket := flag.String("bucket", getenv("PAPER_TRADING_S3_BUCKET", "microservices-trading-bot"), "S3 bucket for snapshots")
	keyPrefix := flag.String("s3-prefix", getenv("PAPER_TRADING_S3_PREFIX", ""), "optional key prefix (e.g. prod/)")
	region := flag.String("region", getenv("AWS_REGION", ""), "AWS region (required for new buckets outside default chain)")
	environment := flag.String("environment", getenv("PAPER_TRADING_ENV", "paper"), "label stored in snapshot JSON, e.g. paper or stage")
	ensureBucket := flag.Bool("ensure-bucket", getenvBool("PAPER_TRADING_ENSURE_BUCKET", true), "create bucket if missing (needs iam:CreateBucket)")
	skipUpload := flag.Bool("dry-run", false, "collect only; print JSON to stdout; no S3 calls")
	flag.Parse()

	if *region == "" && !*skipUpload {
		fmt.Fprintln(os.Stderr, "error: -region or AWS_REGION is required for S3 (unless -dry-run)")
		return 2
	}

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 45 * time.Second}

	snap, err := collector.Collect(ctx, httpClient, *baseURL, *environment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect: %v\n", err)
		return 1
	}

	if *skipUpload {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snap); err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			return 1
		}
		return 0
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		fmt.Fprintf(os.Stderr, "aws config: %v\n", err)
		return 1
	}
	s3Client := s3.NewFromConfig(cfg)

	if *ensureBucket {
		if err := s3export.EnsureBucket(ctx, s3Client, *bucket, *region); err != nil {
			fmt.Fprintf(os.Stderr, "ensure bucket: %v\n", err)
			return 1
		}
	}

	id := uuid.NewString()
	key := s3export.ObjectKey(*keyPrefix, *environment, snap.CollectedAt, id)
	uri, err := s3export.UploadSnapshot(ctx, s3Client, *bucket, key, snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "upload: %v\n", err)
		return 1
	}

	fmt.Println(uri)
	return 0
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}
