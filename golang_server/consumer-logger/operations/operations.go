package operations

import (
	"errors"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/timeseries"
)

const WARMUP_SAMPLES uint8 = 10

func CalculateMovingAverage(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Moving Average...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}
	a := timeseries.New(len(trades))
	var sum float64
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a": // amount
				sum += payload.Amount.Float64()
			case "r": // rate/price
				sum += payload.Price.Float64()
				a.Add(payload.Price.Float64())
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
	log.Println("Calculating Weighted Moving Average...")
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
	log.Println("Calculating Exponential Moving Average...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var ema float64
	var emaInitialized bool

	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				if !emaInitialized {
					ema = payload.Amount.Float64()
					emaInitialized = true
				} else {
					ema = alpha*payload.Amount.Float64() + (1-alpha)*ema
				}
			case "r":
				if !emaInitialized {
					ema = payload.Price.Float64()
					emaInitialized = true
				} else {
					ema = alpha*payload.Price.Float64() + (1-alpha)*ema
				}
			case "v":
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

func CalculateSimpleExponentialWeightedMovingAverage(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Exponential Weighted Moving Average...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	var e timeseries.SimpleEWMA

	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				e.Add(payload.Amount.Float64())
			case "r":
				e.Add(payload.Price.Float64())
			case "v":
				e.Add(payload.Value.Float64())
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return e.Value(), nil
}

func CalculateVariableExponentialWeightedMovingAverage(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Variable Exponential Weighted Moving Average...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	e := timeseries.NewMovingAverage(30)
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				e.Add(payload.Amount.Float64())
			case "r":
				e.Add(payload.Price.Float64())
			case "v":
				e.Add(payload.Value.Float64())
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return e.Value(), nil
}

func CalculateVariableExponentialWeightedMovingAverage2(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Variable Exponential Weighted Moving Average 2...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	e := timeseries.NewMovingAverage(5)
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				e.Add(payload.Amount.Float64())
			case "r":
				e.Add(payload.Price.Float64())
			case "v":
				e.Add(payload.Value.Float64())
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	return e.Value(), nil
}

func CalculateVariableExponentialWeightedMovingAverageWarmUp(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Exponential Weighted Moving Average with WarmUp...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	e := timeseries.NewMovingAverage(5)
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				e.Add(payload.Amount.Float64())
			case "r":
				e.Add(payload.Price.Float64())
				// all values returned during warmup should be 0.0
				if uint8(payload.Price.Float64()) < WARMUP_SAMPLES {
					if e.Value() != 0.0 {
						log.Fatalf("e.Value() is %v, expected %v", e.Value(), 0.0)
					}
				}
			case "v":
				e.Add(payload.Value.Float64())
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	e = timeseries.NewMovingAverage(5)
	e.Set(5)
	e.Add(1)
	if e.Value() >= 5 {
		log.Fatalf("e.Value() is %v, expected it to decay towards 0", e.Value())
	}

	return e.Value(), nil
}
func CalculateVariableExponentialWeightedMovingAverageWarmUp2(trades []bitso.WebsocketTrade, property string) (float64, error) {
	log.Println("Calculating Exponential Weighted Moving Average with WarmUp 2...")
	if len(trades) == 0 {
		return 0, errors.New("empty trade records")
	}

	e := timeseries.NewMovingAverage(15)
	for _, trade := range trades {
		for _, payload := range trade.Payload {
			switch property {
			case "a":
				e.Add(payload.Amount.Float64())
			case "r":
				e.Add(payload.Price.Float64())
				// all values returned during warmup should be 0.0
				if uint8(payload.Price.Float64()) < WARMUP_SAMPLES {
					if e.Value() != 0.0 {
						log.Fatalf("e.Value() is %v, expected %v", e.Value(), 0.0)
					}
				}
			case "v":
				e.Add(payload.Value.Float64())
			default:
				return 0, fmt.Errorf("invalid property: %s", property)
			}
		}
	}

	if val := e.Value(); val == 1.0 {
		log.Fatalf("e.Value() is expected to be greater than %v", 1.0)
	}

	return e.Value(), nil
}
