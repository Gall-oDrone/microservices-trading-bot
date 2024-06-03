package utils

import (
	"fmt"
	"log"
	"math"
	"math/big"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
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

func RoundToNearestTen(input float64) float64 {
	// Round to the nearest unit
	rounded := int(math.Round(input))

	// Round up to the nearest multiple of ten
	remainder := rounded % 10
	if remainder != 0 {
		rounded += 10 - remainder
	}

	return float64(rounded)
}

func ParseErrorMessage(err error) (errorCode int, amount float64, currency string) {
	// Extract the error message from the error
	logMessage := err.Error()

	// Define a regular expression to match the error code
	errorCodeRegex := regexp.MustCompile(`Error (\d+):`)
	errorCodeMatches := errorCodeRegex.FindStringSubmatch(logMessage)
	if errorCodeMatches == nil {
		return 0, 0.0, ""
	}

	// Extract the error code and convert to the appropriate type
	errorCode, _ = strconv.Atoi(errorCodeMatches[1])

	// Depending on the error code, parse the rest of the string
	switch errorCode {
	case 404:
		// Example error: Error 404: Incorrect amount 0.00006 BTC is below the minimum of 0.00075 BTC
		re := regexp.MustCompile(`Error (\d+): Incorrect (\w+) (\d+\.\d+) (\w+) is below the minimum of (\d+\.\d+) (\w+)`)
		matches := re.FindStringSubmatch(logMessage)
		if matches == nil {
			return errorCode, 0.0, ""
		}

		// Extract the matched values and convert to the appropriate types
		amount, _ = strconv.ParseFloat(matches[5], 64)
		currency = matches[2]
		return errorCode, amount, currency

	case 405:
		// Example error: Error 405: Incorrect value $9.70 MXN is below the minimum of $10.00 MXN
		re := regexp.MustCompile(`Incorrect value (\$\d+\.\d+) (\w+) is below the minimum of (\$\d+\.\d+) (\w+)`)
		matches := re.FindStringSubmatch(logMessage)
		if matches == nil {
			return errorCode, 0.0, ""
		}

		// Extract the matched values and convert to the appropriate types
		amount, _ = strconv.ParseFloat(strings.TrimLeft(matches[3], "$"), 64)
		currency = matches[2]
		return errorCode, amount, currency

	case 312:
		// Example error: Error 312: Incorrect OID (non-existent or does not belong to user)  -> bot.BitsoOId:  qmVRxyw0T5qBoDNU
		re := regexp.MustCompile("Incorrect OID  (non-existent or does not belong to user)")
		matches := re.FindStringSubmatch(logMessage)
		if matches == nil {
			return errorCode, 0.0, ""
		}

		// Extract the matched values and convert to the appropriate types
		amount, _ = strconv.ParseFloat(strings.TrimLeft(matches[3], "$"), 64)
		currency = matches[2]
		return errorCode, amount, currency
	default:
		// Handle other error codes as needed
		return errorCode, 0.0, ""
	}
}

func MillisToLocal(timestamp uint64) time.Time {
	// Convert milliseconds to time.Time
	return time.Unix(0, int64(timestamp)*int64(time.Millisecond))

}

func PrintReadableTime(startTimeMillis, endTimeMillis uint64) {
	// Convert milliseconds to time.Time
	startTime := MillisToLocal(startTimeMillis)
	endTime := MillisToLocal(endTimeMillis)

	// Print in human-readable format
	fmt.Printf("Start Time: %s, End Time: %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
}
