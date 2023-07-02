package utils

import (
	"fmt"
	"log"
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
