package utils

import (
	"fmt"
	"log"
	"math/big"
	"math/rand"
	"time"
)

func Rest(max_min int) {
	var minutes int
	log.Println("Server is sleeping")
	for i := 0; i < max_min; i++ {
		time.Sleep(time.Minute)
		minutes++
		log.Printf("%d minute(s) passed.\n", minutes)
	}
	log.Println("Server continuing consuming messages!")
}

func GenRandomId() string {
	rand.Seed(time.Now().UnixNano())

	// Generate a random uint64 number
	randomNumber := uint64(rand.Uint32())<<32 + uint64(rand.Uint32())

	fmt.Println(randomNumber)
	return fmt.Sprint(randomNumber)
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func GenRandomOId(length int) string {
	rand.Seed(time.Now().UnixNano())

	id := make([]rune, length)
	for i := 0; i < length; i++ {
		id[i] = letterRunes[rand.Intn(len(letterRunes))]
	}

	return string(id)
}

func IsTimeInWindow(windowStart, windowEnd time.Time) bool {
	currentTime := time.Now()

	if currentTime.Year() == windowStart.Year() &&
		currentTime.Month() == windowStart.Month() &&
		currentTime.Day() == windowStart.Day() &&
		currentTime.Hour() == windowStart.Hour() &&
		currentTime.Hour() == windowEnd.Hour() {

		return true
	}

	return false

}

func ContainsInt(slice []int, value int) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

func ToBigFloat(value string) {
	// Create a new big.Float with the desired precision (e.g., 64 bits)
	precision := 64
	f := new(big.Float).SetPrec(uint(precision))

	f, _, err := f.Parse(value, 10)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Print the value with the desired precision
	fmt.Printf("Value: %s\n", f.Text('f', -1))
}
