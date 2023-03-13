package strategy

import (
	"fmt"
	"log"

	"github.com/xiam/bitso-go/bitso"
)

// Bid: Highest price a buyer is able to buy
// MajorBidArbitrage is a strategy that compares user's amount of a certain cryptocurrency amount
// against the highest bids registered in Bitso
func MinorAskArbitrage(resp interface{}, client *bitso.Client, target_currency bitso.Currency) {
	balances, err := client.Balances(nil)
	if err != nil {
		log.Fatal("client Balances error: ", err)
	}
	for _, balance := range balances {
		if balance.Currency == target_currency &&
			balance.Available.Float64() > 0 {
			fmt.Printf("Target Currency Balance: %s, %.2f", balance.Currency, balance.Available.Float64())
			trading_amount := PercentageAmountToBid(balance.Available.Float64(), "")
			fees, err := client.Fees(nil)
			if err != nil {
				log.Fatal("client.Fees error: ", err)
			}
			net_profit := BidAfterFee(trading_amount, 1.2, fees)
			log.Print("Net Profit: %s, %.2f", balance.Currency, net_profit)
		}
	}
}

func PercentageAmountToAsk(amount float64, percentage string) float64 {
	switch percentage {
	case "50":
		return amount * (0.5)
	case "75":
		return amount * (0.75)
	default:
		return amount
	}
}

func AskAfterFee(amount, threshold float64, custom_fees *bitso.CustomerFees) float64 {
	var profit float64
	for _, fee := range custom_fees.Fees {
		profit = amount - fee.TakerFeeDecimal.Float64()
		log.Print("Estimated Net Profit: ", profit)
		// if profit > threshold {
		// 	log.Print("Estimated Net Profit: ", profit)
		// }
	}
	return profit
}
