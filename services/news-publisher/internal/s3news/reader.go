package s3news

import (
	"bufio"
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

	"bitso-trading-platform/shared/pkg/models"
)

type Reader struct {
	client *s3.Client
	bucket string
	prefix string
	format string
}

func NewReader(ctx context.Context, bucket, prefix, format string) (*Reader, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &Reader{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		prefix: strings.TrimPrefix(prefix, "/"),
		format: strings.ToLower(format),
	}, nil
}

// LatestETLKey returns the newest day-partitioned ETL object under the configured prefix.
func (r *Reader) LatestETLKey(ctx context.Context) (string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
		Prefix: aws.String(r.prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			key := *obj.Key
			if r.format == "jsonl" && strings.HasSuffix(key, ".jsonl") && strings.Contains(key, "/format=jsonl/") {
				keys = append(keys, key)
			}
			if r.format == "csv" && strings.HasSuffix(key, ".csv") && strings.Contains(key, "/format=csv/") {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("no ETL %s objects under s3://%s/%s", r.format, r.bucket, r.prefix)
	}
	sort.Strings(keys)
	return keys[len(keys)-1], nil
}

// StreamJSONLEvents reads a JSONL object and invokes fn per parsed event.
func (r *Reader) StreamJSONLEvents(ctx context.Context, objectKey string, max int, fn func(models.NewsAgenticEvent) error) error {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("get object %s: %w", objectKey, err)
	}
	defer out.Body.Close()

	scanner := bufio.NewScanner(out.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		if max > 0 && count >= max {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		evt, err := parseJSONLLine(line, objectKey)
		if err != nil {
			continue
		}
		if err := fn(evt); err != nil {
			return err
		}
		count++
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return nil
}

type rawArticle struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Metadata struct {
		Source              string   `json:"source"`
		Datetime            string   `json:"datetime"`
		URL                 string   `json:"url"`
		LLMTicker           string   `json:"llm_ticker"`
		LLMEntities         []string `json:"llm_entities"`
		LLMSignal           string   `json:"llm_signal"`
		LLMOverallSentiment float64  `json:"llm_overall_sentiment"`
		LLMImpactLevel      string   `json:"llm_impact_level"`
		LLMActionable       bool     `json:"llm_actionable"`
		LLMSectors          []string `json:"llm_sectors"`
	} `json:"metadata"`
}

func parseJSONLLine(line, objectKey string) (models.NewsAgenticEvent, error) {
	var raw rawArticle
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return models.NewsAgenticEvent{}, err
	}
	publishedMs := time.Now().UTC().UnixMilli()
	if raw.Metadata.Datetime != "" {
		if t, err := time.Parse(time.RFC3339, raw.Metadata.Datetime); err == nil {
			publishedMs = t.UnixMilli()
		}
	}
	symbol := strings.TrimSpace(raw.Metadata.LLMTicker)
	if symbol == "" && len(raw.Metadata.LLMEntities) > 0 {
		symbol = raw.Metadata.LLMEntities[0]
	}
	return models.NewsAgenticEvent{
		EventID:        fmt.Sprintf("news-%s-%d", raw.ID, publishedMs),
		PublishedAtMs:  publishedMs,
		ArticleID:      raw.ID,
		Title:          raw.Title,
		Source:         raw.Metadata.Source,
		Symbol:         normalizeSymbol(symbol),
		Sectors:        raw.Metadata.LLMSectors,
		Signal:         raw.Metadata.LLMSignal,
		SentimentScore: raw.Metadata.LLMOverallSentiment,
		ImpactLevel:    raw.Metadata.LLMImpactLevel,
		Actionable:     raw.Metadata.LLMActionable,
		URL:            raw.Metadata.URL,
		S3ObjectKey:    objectKey,
	}, nil
}

func normalizeSymbol(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s)
	s = strings.TrimSuffix(s, "-USD")
	return s
}
