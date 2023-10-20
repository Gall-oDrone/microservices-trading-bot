package controllers

import (
	"github.com/segmentio/kafka-go/example/consumer-logger/simulation"
)

func main() {
	// Connect to Redis
	// rdb := redis.NewClient(&redis.Options{
	// 	Addr:     "localhost:6379",
	// 	Password: "",
	// 	DB:       0,
	// })

	// Test Redis connection
	// pong, err := rdb.Ping(rdb.Context()).Result()
	// if err != nil {
	// 	log.Fatalf("Failed to connect to Redis: %v", err)
	// }
	// fmt.Println("Connected to Redis:", pong)

	// Run the trading bot simulation
	simulation.RunSimulation()

	// Close the Redis connection
	// err = rdb.Close()
	// if err != nil {
	// 	log.Printf("Failed to close Redis connection: %v", err)
	// }
}
