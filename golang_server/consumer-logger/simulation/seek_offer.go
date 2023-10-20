package simulation

// TODO make interface

// func seekAskOffer() {
// 	log.Println("Starting bid offers gorutine")
// 	if bot.Side.String() == "sell" {
// 		bot.checkFunds()
// 		params := url.Values{}
// 		params.Set("book", book.String())
// 		orders, err := bot.BitsoClient.OrderBook(params)
// 		if err != nil {
// 			log.Fatalln("Error during open order request: ", err)
// 		}
// 		for _, order := range orders.Bids {
// 			if order.OID != bot.BitsoOId && order.Amount.Float64() == major_amount && order.Price.Float64() >= rate {
// 				order_rate := order.Price
// 				oid := order.OID
// 				order_type := bitso.OrderType(2)
// 				if !bot.Debug {
// 					bot.setOrder(ticker, major_amount, order_rate.Float64(), oid, bitso.OrderStatus(1), order_type, redis_client)
// 				} else {
// 					bot.setUserTrade(rate, major_amount, book_fee.FeeDecimal, book, oid, redis_client)
// 				}
// 				log.Panicln("Ask trade successfully fulfill")
// 				ask_trade <- true
// 			}
// 		}
// 		log.Println("Gorutine bid offers, now sleeping")
// 		time.Sleep(sleep)
// 		log.Println("Gorutine bid offers, now waking")
// 		done <- true
// 	}
// }

// func seekBidOffer() {
// 	log.Println("Starting ask offers gorutine")
// 	if bot.Side.String() == "buy" {
// 		bot.checkFunds()
// 		params := url.Values{}
// 		params.Set("book", book.String())
// 		orders, err := bot.BitsoClient.OrderBook(params)
// 		if err != nil {
// 			log.Fatalln("Error during open order request: ", err)
// 		}
// 		for _, order := range orders.Asks {
// 			if order.OID != bot.BitsoOId && order.Amount.Float64() == minor_amount && order.Price.Float64() >= rate {
// 				order_rate := order.Price
// 				oid := order.OID
// 				if !bot.Debug {
// 					bot.setUserTrade(order_rate.Float64(), minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
// 				} else {
// 					bot.setUserTrade(rate, minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
// 				}
// 				log.Panicln("Buy trade successfully fulfill")
// 				bid_trade <- true
// 			}
// 		}
// 		log.Println("Gorutine ask offers, now sleeping")
// 		time.Sleep(sleep)
// 		log.Println("Gorutine ask offers, now waking")
// 		done <- true
// 	}
// }
