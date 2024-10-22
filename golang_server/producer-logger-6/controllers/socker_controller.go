package controllers

import (
	"fmt"
	"log"

	"github.com/xiam/bitso-go/bitso"
)

var client = bitso.NewClient(nil)

func setClient(bclient *bitso.Client) {
	client = bclient
}

func SellAmountThreshold(book string, threshold, balance, minAmount, maxAmount float64) (float64, error) {
	log.Println("Threshold: ", threshold, ", balance: ", balance, "Book: ", book)
	target_amount := threshold * balance
	if target_amount < minAmount {
		if balance >= minAmount {
			return minAmount, nil
		}
		return balance, fmt.Errorf("client balance amount %0.10f %s is lower than minimum allow: %0.10f %s", balance, book, minAmount, book)
	} else if balance > maxAmount {
		if target_amount <= maxAmount {
			return maxAmount, nil
		}
		return balance, fmt.Errorf("client target amount: %0.10f %s is higher than maximum allow: %0.10f %s", target_amount, book, maxAmount, book)
	}
	return target_amount, nil
}

func SellPrice(book *bitso.Book) (float64, error) {
	ticker, err := client.Ticker(book)
	if err != nil {
		return 0, err
	}
	lowest_ask := ticker.Ask.Float64()  // to buy
	highest_bid := ticker.Bid.Float64() // to sell
	vwap := ticker.Vwap.Float64()
	last := ticker.Last.Float64()
	log.Printf("ticker: %v, lowest ask: %f, highest bid: %f, vwap: %f, last: %f \n", ticker, lowest_ask, highest_bid, vwap, last)
	if highest_bid >= vwap {
		if last >= highest_bid {
			return last, nil
		}
		return highest_bid, nil
	} else if highest_bid < vwap {
		if last < vwap {
			//diff := last + ((vwap - last) * delta_price_limit)
			return vwap, nil
		}
		return last, nil
	}
	return ticker.Last.Float64(), nil
}

func ApplyExchangeFee(price float64, book bitso.Book) (float64, error) {
	customer_fees, err := client.Fees(nil)
	if err != nil {
		log.Fatalln("Error during 'client.Fees(nil) request': ", err)
		// Error 200: Too many requests. You are banned for 24 hours (189.217.87.138)
		// Error 201: Check your credentials
	}
	net_price := price
	for _, fee := range customer_fees.Fees {
		if book.String() == fee.Book.String() {
			fee_decimal_to_float := fee.FeeDecimal.Float64()
			net_price = (price - fee_decimal_to_float)
		}
	}
	return net_price, err
}

func setOrderSide(side string) (order_side string) {
	switch side {
	case "sell":
		//order_side := bitso.OrderSideSell
		return "sell"
	default:
		//order_side := bitso.OrderSideBuy
		return "buy"
	}
}

func setOrderType(order_type string) string {
	//sell_type := bitso.OrderTypeLimit
	return "market"
}

func setMajor(major float64) bitso.Monetary {
	return bitso.ToMonetary(major)
}

func setMinor(minor float64) bitso.Monetary {
	return bitso.ToMonetary(minor)
}

func setPrice(price float64) bitso.Monetary {
	return bitso.ToMonetary(price)
}

func setOrder(
	order_book,
	order_side,
	order_type,
	order_major,
	order_minor,
	order_price string) bitso.OrderPlacement {
	// major := bitso.Book
	return bitso.OrderPlacement{
		// Book=order_book,
		// Side=order_side,
		// Type=order_type,
		// Major=order_major,
		// Minor=order_minor,
		// Price=order_price
	}
}

func placeOrder(amount, price float64, side, o_type string, book bitso.Book) {
	order_book := book
	order_side := setOrderSide(side)
	order_type := setOrderType(o_type)
	order_amount_major := setMajor(amount)
	order_amount_minor := "nil"
	order_price := setPrice(price)
	order := setOrder(
		order_book.Major().String(),
		order_side,
		order_type,
		fmt.Sprintf("%f", order_amount_major.Float64()),
		order_amount_minor,
		fmt.Sprintf("%f", order_price.Float64()),
	)
	log.Println("order: ", order)
	// client.PlaceOrder(*order)
}

func ArbitrageHandler(amount, price_after_fees float64, orders *bitso.WebsocketOrder) {
	book := orders.Book
	order_type := "market"
	side := "sell"
	bids := orders.Payload.Bids
	for _, bid := range bids {
		if amount == bid.Amount && price_after_fees == bid.Price {
			log.Println("book: ", book, ", side: ", side, ", order_type: ", order_type, ", price_after_fees: ", price_after_fees)
			// placeOrder(amount, price_after_fees, side, order_type, book)
		}
	}
}

func ProfitHandler(order *bitso.WebsocketOrder, bclient *bitso.Client, fees *bitso.CustomerFees, balances []bitso.Balance) error {
	var err error
	threshold := 0.25
	setClient(bclient)
	major := order.Book.Major().String() // btc
	minor := order.Book.Minor().String() // btc
	minPrice, maxPrice, minAmount, maxAmount, minValue, maxValue, err := HandlerExchangeOrderBook(client, major)
	if err != nil {
		log.Fatalln("error during handlerExchangeOrderBook: ", err)
	}
	log.Printf("minPrice: $%f, maxPrice: %f, minAmount: %f, maxAmount: %f, minValue: %f, maxValue %f \n", minPrice, maxPrice, minAmount, maxAmount, minValue, maxValue)
	var client_balance bitso.Balance
	for _, balance := range balances {
		if major == balance.Currency.String() {
			client_balance = balance
		}

	}
	available := client_balance.Available.Float64()
	if available > 0 {
		max_amount_to_sell, err := SellAmountThreshold(major, threshold, available, minAmount, maxAmount)
		if err != nil {
			log.Fatalln("error during SellAmountThreshold(): ", err)
		}
		price_to_sell, err := SellPrice(&order.Book)
		if err != nil {
			log.Fatalln("error during SellPrice(): ", err)
		}
		log.Printf("max_amount_to_sell %f %s, at a price $%f %s \n", max_amount_to_sell, major, price_to_sell, minor)
		price_after_fees, err := ApplyExchangeFee(price_to_sell, order.Book)
		if err != nil {
			log.Fatalln("error during ApplyExchangeFee(): ", err)
		}
		log.Printf("Price after fees $%f %s: \n", price_after_fees, minor)
		ArbitrageHandler(max_amount_to_sell, price_after_fees, order)
	}
	return err
}
