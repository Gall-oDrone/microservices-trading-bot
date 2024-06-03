package operations

import (
	"errors"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
)

func CalculateMovingAverage(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Moving Average...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var sum float64
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a": // amount
				sum += payload.Amount.Float64()
			case "r": // rate/price
				sum += payload.Price.Float64()
			case "v": // value
				sum += payload.Value.Float64()
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return sum / float64(len(trades)), nil
}

func CalculateWeightedMovingAverage(trades []bitso.WebsocketTrade, property string) (float64, error) {
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var weightedSum float64
	var totalWeight float64
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				weightedSum += payload.Amount.Float64() * float64(payload.Timestamp)
			case "r":
				weightedSum += payload.Price.Float64() * float64(payload.Timestamp)
			case "v":
				weightedSum += payload.Value.Float64() * float64(payload.Timestamp)
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
			totalWeight += float64(payload.Timestamp)
		}
	}

	return weightedSum / totalWeight, nil
}

func CalculateExponentialMovingAverage(trades []bitso.WebsocketTrade, property string, alpha float64) (float64, error) {
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var ema float64
	var emaInitialized bool

	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "Amount":
				if !emaInitialized {
					ema = payload.Amount.Float64()
					emaInitialized = true
				} else {
					ema = alpha*payload.Amount.Float64() + (1-alpha)*ema
				}
			case "Price":
				if !emaInitialized {
					ema = payload.Price.Float64()
					emaInitialized = true
				} else {
					ema = alpha*payload.Price.Float64() + (1-alpha)*ema
				}
			case "Value":
				if !emaInitialized {
					ema = payload.Value.Float64()
					emaInitialized = true
				} else {
					ema = alpha*payload.Value.Float64() + (1-alpha)*ema
				}
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return ema, nil
}
