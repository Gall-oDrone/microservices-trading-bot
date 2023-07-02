package sleep

import (
	"fmt"
	"log"
	"time"
)

func Rest(seconds, minutes int) {
	if seconds == 0 && minutes == 0 {
		return
	}
	log.Println("Server is sleeping")
	duration := time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
	start := time.Now()
	ticker := time.NewTicker(time.Second)
	fmt.Println("Sleeping...")
	time.Sleep(duration)
	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(start)
			//fmt.Printf("Slept for %d minutes and %d seconds\n", int(elapsed.Minutes()), int(elapsed.Seconds())%60)
			fmt.Printf("Elapsed time: %v\n", elapsed.Round(time.Second))
		case <-time.After(duration):
			fmt.Println("Waking up")
			return
		}
	}
}
