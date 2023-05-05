package controllers

import (
	"github.com/xiam/bitso-go/bitso"
)

func HandlerExchangeOrderBook(client *bitso.Client, book string) (float64, float64, float64, float64, float64, float64, error) {
	var min_price float64
	var max_price float64
	var min_amount float64
	var max_amount float64
	var min_value float64
	var max_value float64
	var err error
	exchangeOrderBook, err := client.AvailableBooks()
	if err != nil {
		return min_price, max_price, min_amount, max_amount, min_value, max_value, err
	}
	for _, exchange_order_book := range exchangeOrderBook {
		if book == exchange_order_book.Book.Major().String() {
			min_amount = exchange_order_book.MinimumAmount.Float64()
			max_amount = exchange_order_book.MaximumAmount.Float64()
			min_price = exchange_order_book.MinimumPrice.Float64()
			max_price = exchange_order_book.MaximumPrice.Float64()
			min_value = exchange_order_book.MinimumValue.Float64()
			max_value = exchange_order_book.MaximumValue.Float64()
		}
	}
	return min_price, max_price, min_amount, max_amount, min_value, max_value, nil
}
