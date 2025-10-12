package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
)

func main() {
	fmt.Println("🚀 Starting Kafka Producer/Consumer Test...")
	fmt.Println("=" + string(make([]byte, 50)) + "=")

	ctx := context.Background()

	// Test 1: Create Producer
	fmt.Println("\n📤 Test 1: Creating Kafka Producer...")
	producerConfig := &kafka.ProducerConfig{
		Brokers:          []string{"localhost:9092"},
		Topic:            "test-topic",
		BatchSize:        10,
		BatchTimeout:     1 * time.Second,
		CompressionCodec: "snappy",
		RequiredAcks:     -1,
		MaxAttempts:      3,
		WriteTimeout:     10 * time.Second,
	}

	producer, err := kafka.NewProducer(producerConfig)
	if err != nil {
		log.Fatalf("❌ Failed to create producer: %v", err)
	}
	defer producer.Close()
	fmt.Println("✅ Producer created successfully!")

	// Test 2: Create Consumer
	fmt.Println("\n📥 Test 2: Creating Kafka Consumer...")
	consumerConfig := &kafka.ConsumerConfig{
		Brokers:         []string{"localhost:9092"},
		Topic:           "test-topic",
		GroupID:         "test-group",
		AutoOffsetReset: "earliest",
		MinBytes:        1,
		MaxBytes:        10e6,
		MaxWait:         500 * time.Millisecond,
		CommitInterval:  1 * time.Second,
	}

	consumer, err := kafka.NewConsumer(consumerConfig)
	if err != nil {
		log.Fatalf("❌ Failed to create consumer: %v", err)
	}
	defer consumer.Close()
	fmt.Println("✅ Consumer created successfully!")

	// Test 3: Send Messages
	fmt.Println("\n📤 Test 3: Sending test messages...")
	testMessages := []models.TradeSignalEvent{
		{
			EventID:   "test-1",
			Timestamp: time.Now().Unix(),
			Book:      "btc_mxn",
			Strategy:  "test-strategy",
			Signal:    "BUY",
			Price:     1000000.50,
			Amount:    0.001,
			Metadata: map[string]interface{}{
				"reason":     "test signal",
				"confidence": 0.95,
			},
		},
		{
			EventID:   "test-2",
			Timestamp: time.Now().Unix(),
			Book:      "eth_mxn",
			Strategy:  "test-strategy",
			Signal:    "SELL",
			Price:     50000.25,
			Amount:    0.05,
			Metadata: map[string]interface{}{
				"reason":     "test signal",
				"confidence": 0.90,
			},
		},
		{
			EventID:   "test-3",
			Timestamp: time.Now().Unix(),
			Book:      "btc_mxn",
			Strategy:  "test-strategy",
			Signal:    "BUY",
			Price:     1000100.00,
			Amount:    0.002,
			Metadata: map[string]interface{}{
				"reason":     "test signal",
				"confidence": 0.88,
			},
		},
	}

	messagesSent := 0
	for i, signal := range testMessages {
		data, err := json.Marshal(signal)
		if err != nil {
			log.Printf("❌ Failed to marshal message %d: %v", i+1, err)
			continue
		}

		key := []byte(signal.EventID)
		produceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = producer.Produce(produceCtx, key, data)
		cancel()

		if err != nil {
			log.Printf("❌ Failed to send message %d: %v", i+1, err)
			continue
		}

		messagesSent++
		fmt.Printf("  ✅ Sent message %d: %s %s for %s at %.2f (Amount: %.4f)\n",
			i+1, signal.Signal, signal.Book, signal.EventID, signal.Price, signal.Amount)
	}

	fmt.Printf("\n📊 Total messages sent: %d/%d\n", messagesSent, len(testMessages))

	// Wait a moment for messages to be available
	fmt.Println("\n⏳ Waiting 2 seconds for messages to be available...")
	time.Sleep(2 * time.Second)

	// Test 4: Consume Messages
	fmt.Println("\n📥 Test 4: Consuming messages...")
	messagesReceived := 0
	timeout := time.After(10 * time.Second)

	for messagesReceived < messagesSent {
		select {
		case <-timeout:
			fmt.Printf("\n⚠️  Timeout reached. Received %d/%d messages\n", messagesReceived, messagesSent)
			goto summary

		default:
			consumeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			data, err := consumer.Consume(consumeCtx)
			cancel()

			if err != nil {
				if err == context.DeadlineExceeded {
					continue
				}
				log.Printf("❌ Error consuming message: %v", err)
				continue
			}

			var signal models.TradeSignalEvent
			if err := json.Unmarshal(data, &signal); err != nil {
				log.Printf("❌ Failed to unmarshal message: %v", err)
				continue
			}

			messagesReceived++
			fmt.Printf("  ✅ Received message %d: %s %s for %s at %.2f (Amount: %.4f)\n",
				messagesReceived, signal.Signal, signal.Book, signal.EventID, signal.Price, signal.Amount)
		}
	}

summary:
	// Test 5: Summary
	fmt.Println("\n" + string(make([]byte, 60)))
	fmt.Println("📊 TEST SUMMARY")
	fmt.Println(string(make([]byte, 60)))
	fmt.Printf("Messages Sent:     %d\n", messagesSent)
	fmt.Printf("Messages Received: %d\n", messagesReceived)

	if messagesSent == messagesReceived && messagesSent > 0 {
		fmt.Println("\n🎉 SUCCESS! All messages were successfully sent and received!")
		fmt.Println("✅ Kafka Producer/Consumer are working correctly!")
	} else if messagesReceived > 0 {
		fmt.Println("\n⚠️  PARTIAL SUCCESS! Some messages were received but not all.")
		fmt.Printf("   Expected: %d, Got: %d\n", messagesSent, messagesReceived)
	} else {
		fmt.Println("\n❌ FAILED! No messages were received.")
	}

	// Get and display stats
	fmt.Println("\n📈 Producer Stats:")
	producerStats := producer.Stats()
	fmt.Printf("  Messages: %d\n", producerStats.Messages)
	fmt.Printf("  Bytes: %d\n", producerStats.Bytes)
	fmt.Printf("  Errors: %d\n", producerStats.Errors)

	fmt.Println("\n📈 Consumer Stats:")
	consumerStats := consumer.Stats()
	fmt.Printf("  Messages: %d\n", consumerStats.Messages)
	fmt.Printf("  Bytes: %d\n", consumerStats.Bytes)

	fmt.Println("\n✨ Test completed!")
}
