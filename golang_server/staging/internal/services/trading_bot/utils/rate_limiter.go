package utils

import (
	"log"
	"time"
)

// RateLimiter handles API request rate limiting
type RateLimiter struct {
	MaxPublicAPIRequestsPerMinute  int
	MaxPrivateAPIRequestsPerMinute int
}

// NewRateLimiter creates a new rate limiter instance
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		MaxPublicAPIRequestsPerMinute:  60,
		MaxPrivateAPIRequestsPerMinute: 300,
	}
}

// UpdatePublicAPIRequest updates the public API request counter
func (rl *RateLimiter) UpdatePublicAPIRequest() {
	rl.handleMaxPublicAPIRequestPerMinute()
	rl.MaxPublicAPIRequestsPerMinute--
}

// UpdatePrivateAPIRequest updates the private API request counter
func (rl *RateLimiter) UpdatePrivateAPIRequest() {
	rl.handleMaxPrivateAPIRequestPerMinute()
	rl.MaxPrivateAPIRequestsPerMinute--
}

// handleMaxPublicAPIRequestPerMinute handles when public API limit is reached
func (rl *RateLimiter) handleMaxPublicAPIRequestPerMinute() {
	sleepDuration := 1 * time.Hour
	if rl.MaxPublicAPIRequestsPerMinute == 0 {
		log.Printf("Max Public API requests reached!, bot will sleeping %v hours \n", sleepDuration)
		time.Sleep(sleepDuration)
		log.Println("Max Public API requests restored!, bot will keep trading")
	}
}

// handleMaxPrivateAPIRequestPerMinute handles when private API limit is reached
func (rl *RateLimiter) handleMaxPrivateAPIRequestPerMinute() {
	sleepDuration := 1 * time.Hour
	if rl.MaxPrivateAPIRequestsPerMinute == 0 {
		log.Printf("Max Private API requests reached!, bot will sleeping %v hours \n", sleepDuration)
		time.Sleep(sleepDuration)
		log.Println("Max Private API requests restored!, bot will keep trading")
	}
}

// ResetAPIRequests resets the API request counters every minute
func (rl *RateLimiter) ResetAPIRequests() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.MaxPublicAPIRequestsPerMinute = 60
			rl.MaxPrivateAPIRequestsPerMinute = 300
		}
	}
}
