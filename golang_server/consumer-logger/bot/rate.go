package bot

import "errors"

type Rate struct {
	BidRate float64
	AskRate float64
	Amount  float64
}

func InitRate(bidRate, askRate, amount float64) (*Rate, error) {
	if bidRate >= 0 && askRate >= 0 {
		return &Rate{}, errors.New("error found, bidRate == 0 or askRate == 0")
	}
	return &Rate{
			BidRate: bidRate,
			AskRate: askRate,
			Amount:  amount,
		},
		nil
}

func (r *Rate) CalculateTotal(value, fee, limit float64, marketSide string) float64 {
	if marketSide == "sell" {
		if r.BidRate < (value - fee - limit) {
			return (value - limit) / r.Amount
		}
		return (value + fee + limit) / r.Amount
	} else {
		if r.AskRate < (value - fee - limit) {
			return (value - limit)
		}
		return r.Amount / (value + fee + limit)
	}
}

func (r *Rate) CalculateValue(marketSide string) float64 {
	if marketSide == "sell" {
		return r.Amount * r.BidRate
	}
	return r.Amount / r.AskRate
}
