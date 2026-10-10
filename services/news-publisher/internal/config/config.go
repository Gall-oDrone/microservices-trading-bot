package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host              string
	Port              int
	S3Bucket          string
	S3Prefix          string
	S3Format          string
	KafkaBrokers      string
	KafkaTopic        string
	PublishInterval   time.Duration
	MaxArticlesPerRun int
}

func Load() Config {
	interval := 5 * time.Minute
	if v := os.Getenv("NEWS_PUBLISH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	maxArticles := 500
	if v := os.Getenv("NEWS_MAX_ARTICLES_PER_RUN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxArticles = n
		}
	}
	port := 8090
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	return Config{
		Host:              envOr("HOST", "0.0.0.0"),
		Port:              port,
		S3Bucket:          envOr("NEWS_S3_BUCKET", "test-financial-news-bucket"),
		S3Prefix:          envOr("NEWS_S3_PREFIX", "news/transformed/crypto/agentic=true/"),
		S3Format:          envOr("NEWS_S3_FORMAT", "jsonl"),
		KafkaBrokers:      envOr("KAFKA_BROKERS", "localhost:9092"),
		KafkaTopic:        envOr("KAFKA_TOPIC_NEWS_AGENTIC", "news.agentic"),
		PublishInterval:   interval,
		MaxArticlesPerRun: maxArticles,
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
