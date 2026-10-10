package models

// NewsAgenticEvent is published from S3 ETL JSONL via news-publisher (topic: news.agentic).
type NewsAgenticEvent struct {
	EventID         string   `json:"event_id"`
	PublishedAtMs   int64    `json:"published_at_ms"`
	ArticleID       string   `json:"article_id"`
	Title           string   `json:"title"`
	Source          string   `json:"source,omitempty"`
	Symbol          string   `json:"symbol,omitempty"`
	Sectors         []string `json:"sectors,omitempty"`
	Signal          string   `json:"signal,omitempty"` // bullish, bearish, neutral
	SentimentScore  float64  `json:"sentiment_score"`
	ImpactLevel     string   `json:"impact_level,omitempty"`
	Actionable      bool     `json:"actionable"`
	URL             string   `json:"url,omitempty"`
	S3ObjectKey     string   `json:"s3_object_key,omitempty"`
}

// ResearchMemoEvent is a cold-path output from TradingAgents (topic: research.memos).
// It must not be consumed directly for live order placement without an approval gate.
type ResearchMemoEvent struct {
	RunID           string                 `json:"run_id"`
	TimestampMs     int64                  `json:"timestamp_ms"`
	Ticker          string                 `json:"ticker"`
	TradeDate       string                 `json:"trade_date"`
	Decision        string                 `json:"decision"`
	Framework       string                 `json:"framework"` // trading-agents
	BrokerTarget    string                 `json:"broker_target,omitempty"` // etoro, bitso, none
	NewsContextUsed bool                   `json:"news_context_used"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}
