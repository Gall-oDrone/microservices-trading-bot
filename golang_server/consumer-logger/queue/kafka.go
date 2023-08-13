package queue

import (
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
