package sleep

import (
	"log"
	"time"
)

func Rest(max_min int) {
	if max_min == 0 {
		return
	}
	var minutes int
	log.Println("Server is sleeping")
	for i := 0; i < max_min; i++ {
		time.Sleep(time.Minute)
		minutes++
		log.Printf("%d minute(s) passed.\n", minutes)
	}
	log.Println("Server continuing consuming messages!")
}
