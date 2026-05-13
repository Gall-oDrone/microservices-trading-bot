// Command paper-trading-reporter collects strategy-executor state (paper / organic runs)
// and uploads a JSON snapshot to S3. Optional: ensure the bucket exists (HeadBucket + CreateBucket).
//
// Example:
//
//	AWS_REGION=us-east-1 go run ./cmd/paper-trading-reporter \
//	  -strategy-executor-url http://127.0.0.1:8084 \
//	  -bucket microservices-trading-bot \
//	  -environment paper \
//	  -bitso-api-base-url https://stage.bitso.com/api \
//	  -bitso-api-key "$BITSO_API_KEY" \
//	  -bitso-api-secret "$BITSO_API_SECRET"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	bitsoBase := flag.String("bitso-api-base-url", getenv("BITSO_API_BASE_URL", ""), "optional; Bitso API prefix for GET /fees (e.g. https://stage.bitso.com/api)")
	bitsoKey := flag.String("bitso-api-key", getenv("BITSO_API_KEY", ""), "optional; Bitso key for GET /fees when estimating limit_profit thresholds")
	bitsoSecret := flag.String("bitso-api-secret", getenv("BITSO_API_SECRET", ""), "optional; Bitso secret for GET /fees")
	lpEstBuy := flag.Float64("lp-estimate-buy-fee-decimal", getenvFloat("PAPER_LP_ESTIMATE_BUY_FEE_DECIMAL", 0), "optional global override buy-leg fee decimal (e.g. 0.0057); skips Bitso fetch when both overrides > 0")
	lpEstSell := flag.Float64("lp-estimate-sell-fee-decimal", getenvFloat("PAPER_LP_ESTIMATE_SELL_FEE_DECIMAL", 0), "optional global override sell-leg fee decimal (e.g. 0.00741)")
	flag.Parse()

	if *region == "" && !*skipUpload {
		fmt.Fprintln(os.Stderr, "error: -region or AWS_REGION is required for S3 (unless -dry-run)")
		return 2
	}

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 45 * time.Second}

	feeOpts := collector.FeeEstimateOptions{
		BitsoBaseURL:           strings.TrimSpace(*bitsoBase),
		BitsoKey:               strings.TrimSpace(*bitsoKey),
		BitsoSecret:            strings.TrimSpace(*bitsoSecret),
		OverrideBuyFeeDecimal:  *lpEstBuy,
		OverrideSellFeeDecimal: *lpEstSell,
	}
	snap, err := collector.Collect(ctx, httpClient, *baseURL, *environment, feeOpts)
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

func getenvFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
