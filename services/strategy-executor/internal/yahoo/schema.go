// Package yahoo compacts the Yahoo Finance crypto price and LLM-scored news
// partitions in S3 into one deduplicated Parquet file per dataset, and reads
// those files back.
//
// Why the partitions need deduplicating (measured 2026-09-26, see
// cmd/yahoo-compact):
//
//   - Prices (s3://test-financial-stocks-bucket/stocks/crypto/) are written in
//     two layouts — <book>/YYYY/MM/DD/ and book=<book>/year=/month=/day=[/format=csv]/
//     — plus multi-book *_full_record.csv snapshots. The same (book, date) bar
//     can appear in several of them, and a bar fetched before the UTC day
//     closed is later re-fetched with different (final) values.
//   - News (s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/)
//     is written as daily CSV, a daily JSONL copy with a different id scheme,
//     and weekly/monthly/yearly CSV roll-ups that repeat the daily rows. Some
//     articles exist ONLY in a roll-up or ONLY in the JSONL copy, so no single
//     layout can be dropped wholesale.
//
// The news `id` column is NOT an article key: hundreds of ids are shared by
// 2..10+ different articles. The canonical article URL is the key instead.
package yahoo

import (
	"time"
)

// PriceRow is one daily OHLCV bar. Field order and tags define the Parquet
// schema of the compacted stocks file.
type PriceRow struct {
	Book     string  `parquet:"name=book, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	Date     int32   `parquet:"name=date, type=INT32, convertedtype=DATE"` // days since 1970-01-01 (UTC)
	Open     float64 `parquet:"name=open, type=DOUBLE"`
	High     float64 `parquet:"name=high, type=DOUBLE"`
	Low      float64 `parquet:"name=low, type=DOUBLE"`
	Close    float64 `parquet:"name=close, type=DOUBLE"`
	AdjClose float64 `parquet:"name=adj_close, type=DOUBLE"`
	Volume   *int64  `parquet:"name=volume, type=INT64, repetitiontype=OPTIONAL"`
	// Ref is the data vendor (always https://finance.yahoo.com today); kept so
	// the compacted file is a lossless superset of the source rows.
	Ref string `parquet:"name=ref, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY"`
	// CreatedAt is when the collector wrote the row; only files written since
	// ~2026-06 carry it.
	CreatedAt *int64 `parquet:"name=created_at, type=INT64, convertedtype=TIMESTAMP_MILLIS, repetitiontype=OPTIONAL"`
	// SourceKey is the S3 key (relative to the compacted prefix root) of the
	// object this row was taken from, for lineage.
	SourceKey string `parquet:"name=source_key, type=BYTE_ARRAY, convertedtype=UTF8"`
}

// Day returns the bar's date as a UTC midnight time.
func (p PriceRow) Day() time.Time { return DateFromDays(p.Date) }

// NewsRow is one LLM-scored article. Field order and tags define the Parquet
// schema of the compacted news file. Column names match the transformed CSV
// so existing consumers need no renaming; nullable values are pointers.
type NewsRow struct {
	// Href is the canonical article URL (tracking query and fragment removed).
	// It is the deduplication key and is unique within the file.
	Href     string  `parquet:"name=href, type=BYTE_ARRAY, convertedtype=UTF8"`
	ID       string  `parquet:"name=id, type=BYTE_ARRAY, convertedtype=UTF8"`
	Datetime *int64  `parquet:"name=datetime, type=INT64, convertedtype=TIMESTAMP_MILLIS, repetitiontype=OPTIONAL"`
	Source   *string `parquet:"name=source, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	Headline *string `parquet:"name=headline, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	Summary  *string `parquet:"name=summary, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	Content  *string `parquet:"name=content, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`
	Author   *string `parquet:"name=author, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	MinsRead *string `parquet:"name=minsread, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	// CreatedAt is when the transform pipeline wrote the row (only present in
	// the newer CSV schema).
	CreatedAt *int64 `parquet:"name=created_at, type=INT64, convertedtype=TIMESTAMP_MILLIS, repetitiontype=OPTIONAL"`

	LLMFinancialMetrics *string  `parquet:"name=llm_financial_metrics, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"` // JSON object text
	LLMEntities         *string  `parquet:"name=llm_entities, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`          // JSON array text
	LLMTicker           *string  `parquet:"name=llm_ticker, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMEventType        *string  `parquet:"name=llm_event_type, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMOverallSentiment *float64 `parquet:"name=llm_overall_sentiment, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMForwardSentiment *float64 `parquet:"name=llm_forward_sentiment, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMSurpriseScore    *float64 `parquet:"name=llm_surprise_score, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMRiskScore        *float64 `parquet:"name=llm_risk_score, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMUncertaintyScore *float64 `parquet:"name=llm_uncertainty_score, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMImpactStrength   *float64 `parquet:"name=llm_impact_strength, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMImmediacy        *float64 `parquet:"name=llm_immediacy, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMImpactHorizon    *string  `parquet:"name=llm_impact_horizon, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMConfidence       *float64 `parquet:"name=llm_confidence, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMNoveltyScore     *float64 `parquet:"name=llm_novelty_score, type=DOUBLE, repetitiontype=OPTIONAL"`
	LLMSentimentLabel   *string  `parquet:"name=llm_sentiment_label, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMImpactLevel      *string  `parquet:"name=llm_impact_level, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMSignal           *string  `parquet:"name=llm_signal, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`
	LLMActionable       *bool    `parquet:"name=llm_actionable, type=BOOLEAN, repetitiontype=OPTIONAL"`
	LLMSectors          *string  `parquet:"name=llm_sectors, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"`   // JSON array text
	LLMKeyFacts         *string  `parquet:"name=llm_key_facts, type=BYTE_ARRAY, convertedtype=UTF8, repetitiontype=OPTIONAL"` // JSON array text
	LLMError            *string  `parquet:"name=llm_error, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN_DICTIONARY, repetitiontype=OPTIONAL"`

	SourceKey string `parquet:"name=source_key, type=BYTE_ARRAY, convertedtype=UTF8"`
}

// Time returns the article's publication time, or the zero time if unknown.
func (n NewsRow) Time() time.Time {
	if n.Datetime == nil {
		return time.Time{}
	}
	return time.UnixMilli(*n.Datetime).UTC()
}

// Scored reports whether the LLM produced a usable sentiment for the article.
func (n NewsRow) Scored() bool { return n.LLMOverallSentiment != nil }

// DaysFromDate converts a date to the Parquet DATE representation.
func DaysFromDate(t time.Time) int32 {
	y, m, d := t.Date()
	return int32(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// DateFromDays converts a Parquet DATE value back to a UTC midnight time.
func DateFromDays(d int32) time.Time { return time.Unix(int64(d)*86400, 0).UTC() }
