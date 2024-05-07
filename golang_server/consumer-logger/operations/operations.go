package operations

import (
	"errors"
	"fmt"

	"github.com/xiam/bitso-go/bitso"
)

func CalculateMovingAverage(trades []bitso.bitso.WebsocketTrade, property string) (float64, error) {
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var sum float64
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "Amount":
				sum += payload.Amount
			case "Price":
				sum += payload.Price
			case "Value":
				sum += payload.Value
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
			case "Amount":
				weightedSum += payload.Amount * float64(payload.Timestamp)
			case "Price":
				weightedSum += payload.Price * float64(payload.Timestamp)
			case "Value":
				weightedSum += payload.Value * float64(payload.Timestamp)
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
			totalWeight += float64(payload.Timestamp)
		}
	}

	return weightedSum / totalWeight, nil
}

func (rc *RedisClient) CalculateExponentialMovingAverage(trades []bitso.WebsocketTrade, property string, alpha float64) (float64, error) {
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
					ema = payload.Amount
					emaInitialized = true
				} else {
					ema = alpha*payload.Amount + (1-alpha)*ema
				}
			case "Price":
				if !emaInitialized {
					ema = payload.Price
					emaInitialized = true
				} else {
					ema = alpha*payload.Price + (1-alpha)*ema
				}
			case "Value":
				if !emaInitialized {
					ema = payload.Value
					emaInitialized = true
				} else {
					ema = alpha*payload.Value + (1-alpha)*ema
				}
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return ema, nil
}
