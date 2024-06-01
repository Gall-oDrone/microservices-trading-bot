package queue

import (
	"fmt"
	"math"
	"strings"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

type KafkaClient struct {
	Topic   string
	GroupId string
	Reader  *kafka.Reader
	Writer  *kafka.Writer
}

type KafkaConsumer struct {
	Window      Window    `json:"window"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	NextMinute  time.Time `json:"next_minute"`
	AvgVolume   float64   `json:"avg_volume"`
	AvgHigh     float64   `json:"avg_high"`
	AvgLow      float64   `json:"avg_low"`
	AvgLast     float64   `json:"avg_last"`
	AvgVWap     float64   `json:"avg_vwap"`
	AvgBid      float64   `json:"avg_bid"`
	BidStdDev   float64   `json:"bid_stddev"`
	AvgAsk      float64   `json:"avg_ask"`
	AskStdDev   float64   `json:"ask_stddev"`
}

type BidTradeTrendConsumer struct {
	BatchId       int       `json:"batch_id"`
	Side          int       `json:"side"`
	Index         int       `json:"index"`
	Window        Window    `json:"window"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	MaxPrice      float64   `json:"max_price"`
	MinPrice      float64   `json:"min_price"`
	Var           float64   `json:"var"`
	StdDev        float64   `json:"stddev"`
	MovingAverage float64   `json:"moving_avg"`
	Trend         int8      `json:"trend"`
}

type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func NewKafkaClient(kafkaURL, topic, groupId string) *KafkaClient {
	writer := getKafkaWriter(kafkaURL, topic)
	reader := getKafkaReader(kafkaURL, topic, groupId)
	return &KafkaClient{
		Topic:   topic,
		GroupId: groupId,
		Reader:  reader,
		Writer:  writer,
	}
}

func getKafkaWriter(kafkaURL, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(kafkaURL),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
}

func getKafkaReader(kafkaURL, topic, groupID string) *kafka.Reader {
	brokers := strings.Split(kafkaURL, ",")
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     groupID,
		Topic:       topic,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
		StartOffset: kafka.LastOffset,
	})
}

func GetMaxTrend(lastTwoBatches []BidTradeTrendConsumer) (int8, error) {
	trendCounts := make(map[int8]int)

	// Count the occurrences of each Trend value
	for _, batch := range lastTwoBatches {
		trendCounts[batch.Trend]++
	}

	// Find the value with the maximum count
	var maxTrend int8
	maxCount := 0
	for trend, count := range trendCounts {
		if count > maxCount {
			maxCount = count
			maxTrend = trend
		}
	}

	if maxCount == 0 {
		return 0, fmt.Errorf("no trend data found")
	}

	return maxTrend, nil
}

func CalculateCumulativePercentageChange(batches []BidTradeTrendConsumer) float64 {
	var cumulativePercentageChange float64

	for i := 1; i < len(batches); i++ {
		previousBatch := batches[i-1]
		currentBatch := batches[i]

		// Calculate the percentage change in MovingAverage
		percentageChange := ((currentBatch.MovingAverage - previousBatch.MovingAverage) / previousBatch.MovingAverage) * 100

		cumulativePercentageChange += percentageChange
	}

	return math.Round(cumulativePercentageChange*100) / 100
}
