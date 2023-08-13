package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/queue"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
)

var wg sync.WaitGroup
var mutex sync.Mutex

type TradingBot struct {
	BitsoClient                *bitso.Client
	DBClient                   interface{}
	KafkaClient                *queue.KafkaClient
	BitsoBook                  bitso.Book
	BitsoExchangeBook          bitso.ExchangeOrderBook
	BitsoOId                   string
	BitsoActiveOIds            []string
	MajorBalance               bitso.Balance
	MinorBalance               bitso.Balance
	MajorActiveOrders          []string
	MinorActiveOrders          []string
	Side                       bitso.OrderSide
	Threshold                  float64
	OrderSet                   chan bool
	MajorProfit                TradingProfit
	MinorProfit                TradingProfit
	MaxPublicRequestPerMinute  int8
	MaxPrivateRequestPerMinute int8
	Debug                      bool
}

type TradingProfit struct {
	Currency         bitso.Currency
	Initial          float64
	Last             float64
	Cumulative       float64
	Change           float64
	CumulativeChange float64
	UpdatedAt        time.Time
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 4, 4, 3, ' ', 0)
}

func NewTradingBot(b_client *bitso.Client, db_client interface{}, book *bitso.Book) *TradingBot {
	return &TradingBot{
		BitsoClient:                b_client,
		DBClient:                   db_client,
		BitsoBook:                  *book,
		MaxPublicRequestPerMinute:  60,
		MaxPrivateRequestPerMinute: 100,
		Side:                       bitso.OrderSide(1),
		Threshold:                  0.1,
		Debug:                      true,
	}
}

func RunSimulation() {
	bitso_client := bitso.NewClient(nil)
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("error during redis setup: ", err)
	}
	major := bitso.ToCurrency("btc")
	minor := bitso.ToCurrency("mxn")
	book := bitso.NewBook(major, minor)
	bot := NewTradingBot(bitso_client, redis_client, book)
	bot.setBitsoClient()
	bot.setInitialConfg()

	bot.generateAskTrade()
	// go bot.generateBidTrade()
	// Run the simulation for a certain period
	endTime := time.Now().Add(12 * time.Hour) // Simulation for 12 hours
	for time.Now().Before(endTime) {
		log.Println("Simulation started at:", time.Now())
		log.Println("Simulation will ends at:", endTime)
		// Sleep for a random duration between trades
		log.Println("Simulation now slepping!")
		sleepDuration := 1 * time.Minute
		time.Sleep(sleepDuration)
	}
}

func (bot *TradingBot) setInitialConfg() {
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("Failed to connect to Redis: ", err)
	}
	bot.DBClient = redis_client
	defer redis_client.CloseDB()
	bot.setKafkaClient()
	bot.setInitBalance(redis_client)
	bot.setInitSide()
	bot.setInitFees(redis_client)
	bot.setInitExchangeOrderBooks(redis_client)
	// bot.setInitTrade(redis_client)
}

func (bot *TradingBot) setInitSide() {
	minor_available := bot.MinorBalance.Available.Float64()
	major_available := bot.MajorBalance.Available.Float64()

	if minor_available > 0.0 && major_available <= 0.0 {
		bot.Side = bitso.OrderSide(1)
		return
	} else if major_available > 0.0 && minor_available <= 0.0 {
		bot.Side = bitso.OrderSide(2)
		return
	} else if major_available > 0.0 && minor_available > 0.0 {
		bot.Side = bitso.OrderSide(1)
		return
	}
}

func (bot *TradingBot) setInitFees(redis_client *database.RedisClient) {
	fees, err := bot.BitsoClient.Fees(nil)
	if err != nil {
		log.Fatalln("Error during Fees request: ", err)
	}

	err = redis_client.SaveFees(fees.Fees)
	if err != nil {
		log.Fatalln("Error saving fees: ", err)
	}
}

func (bot *TradingBot) setInitExchangeOrderBooks(redis_client *database.RedisClient) {
	exchange_order_books, err := bot.BitsoClient.AvailableBooks()
	if err != nil {
		log.Fatal("Error requesting exchange order books: ", err)
	}
	err = redis_client.SaveExchangeOrderBooks(exchange_order_books)
	if err != nil {
		log.Fatalln("Error saving fees: ", err)
	}
	bot.BitsoExchangeBook, err = redis_client.GetExchangeOrderBook(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting exchange order book: ", err)
	}
}

func (bot *TradingBot) setInitBalance(redis_client *database.RedisClient) {
	if !bot.Debug {
		balances, err := bot.BitsoClient.Balances(nil)
		if err != nil {
			log.Fatal("Error requesting user balances: ", err)
		}
		for _, balance := range balances {
			if balance.Currency == bot.BitsoBook.Major() {
				bot.MajorBalance = balance
				err = redis_client.SaveUserBalance(&balance)
				if err != nil {
					log.Fatal("Error saving user balance: ", err)
				}
			} else if balance.Currency == bot.BitsoBook.Minor() {
				bot.MinorBalance = balance
				err = redis_client.SaveUserBalance(&balance)
				if err != nil {
					log.Fatal("Error saving user balance: ", err)
				}
			}
		}
		minor_available := bot.MinorBalance.Available.Float64()
		major_available := bot.MajorBalance.Available.Float64()
		if major_available == 0.0 && minor_available == 0.0 {
			// TODO deposit and fund MXN
			log.Panic("TODO please add funds to this account")
		}
	} else {
		bot.MajorBalance = setBalance(0.0, 0.0, "BTC")
		err := redis_client.SaveUserBalance(&bot.MajorBalance)
		if err != nil {
			log.Fatal("Error saving user balance: ", err)
		}
		bot.MinorBalance = setBalance(1000.0, 0.0, "MXN")
		err = redis_client.SaveUserBalance(&bot.MinorBalance)
		if err != nil {
			log.Fatal("Error saving user balance: ", err)
		}
	}
}

func (bot *TradingBot) setInitTrade(redis_client *database.RedisClient) {
	book_fee, err := redis_client.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting book fee: ", err)
	}

	ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error during ticker request: ", err)
	}

	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	bitso_fee := book_fee.FeeDecimal
	oid := utils.GenRandomOId(7)

	if !bot.Debug {
		if bot.Side.String() == "sell" {
			log.Fatalln("Test 1")
			// bot.MinorActiveOrders = append(bot.MinorActiveOrders, bot.BitsoOId)
		} else {
			amount := bot.getMinMinorValue()
			order_type := bitso.OrderType(2)
			market_trade := "maker"
			rate := bot.getOptimalPrice(ticker, market_trade, bot.Side.String())
			currency_amount := amount / rate
			// stopSimulation()
			bot.setOrder(ticker, currency_amount, rate, order_type, redis_client)
			bot.MajorActiveOrders = append(bot.MajorActiveOrders, bot.BitsoOId)
			bot.OrderSet <- true
		}
	} else {
		if bot.Side.String() == "sell" {
			amount := bot.getMinMinorValue() / bid_rate
			bot.setUserTrade(bid_rate, amount, bitso_fee, bot.BitsoBook, oid, redis_client)
		} else {
			amount := bot.getMinMinorValue()
			bot.setUserTrade(ask_rate, amount, bitso_fee, bot.BitsoBook, oid, redis_client)
		}
	}
}

func (bot *TradingBot) generateAskTrade() {
	var (
		major_amount float64
		minor_amount float64
		rate         float64
	)
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("Failed to connect to Redis:", err)
	}
	defer redis_client.CloseDB()
	book := bot.BitsoBook
	market_trade := "taker"
	if bot.Side.String() == "sell" && bot.MajorBalance.Available.Float64() <= 0.0 || bot.Side.String() == "buy" && bot.MinorBalance.Available.Float64() <= 0.0 {
		log.Fatalln("error user balance is 0: ", err)
	}
	book_fee, err := redis_client.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting book fee: ", err)
	}
	ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
	if err != nil {
		log.Fatalln("error getting ticket request")
	}
	rate = bot.getOptimalPrice(ticker, market_trade, bot.Side.String())
	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	major_amount = bot.getMinMinorValue() / bid_rate
	minor_amount = bot.getMinMinorValue()
	done := make(chan bool)
	kafka_avg_price_task_done := make(chan bool)
	// ask_trade := make(chan bool)
	// bid_trade := make(chan bool)
	order_set := make(chan bool)
	kafka_avg_price := make(chan queue.KafkaConsumer)
	// kafka_price_trend := make(chan queue.KafkaConsumer)
	// var k_wg sync.WaitGroup
	// go bot.KafkaAvgPricesConsumer(kafka_avg_price, &k_wg)
	// go bot.KafkaPriceTrendConsumer(kafka_price_trend, &k_wg)
	// k_wg.Wait()
	// log.Println("end kafka now continue!")
	// sleep := (10 * time.Second)
	// wg.Add(1)
	log.Println("Start")
	for {
		// go func() {
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
		// 					bot.setOrder(ticker, major_amount, order_rate.Float64(), order_type, redis_client)
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
		// }()
		// go func() {
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
		// }()
		go func() {
			time_ticker := time.NewTicker(30 * time.Second)
			defer time_ticker.Stop()
			for range time_ticker.C {
				log.Println("30 seconds have passed")
				bot.checkFunds()
				ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
				if err != nil {
					log.Fatalln("error getting ticket request")
				}
				ask_rate = ticker.Ask.Float64()
				bid_rate = ticker.Bid.Float64()
				market_trade = "maker"
				rate = bot.getOptimalPrice(ticker, market_trade, bot.Side.String())
				major_amount = bot.getMinMinorValue() / bid_rate
				minor_amount = bot.getMinMinorValue()
				order_type := bitso.OrderType(2)
				x := 0.5
				rand.Seed(time.Now().UnixNano())
				randomNumber := rand.Float64()
				oid := utils.GenRandomOId(7)
				if !bot.Debug {
					mutex.Lock()
					if bot.Side.String() == "buy" {
						currency_amount := minor_amount / ask_rate // BTC
						log.Println("amount as minor: ", currency_amount*rate)
						bot.setOrder(ticker, currency_amount, rate, order_type, redis_client)
						order_set <- true
					} else {
						currency_amount := major_amount * bid_rate // MXN
						bot.setOrder(ticker, currency_amount, rate, order_type, redis_client)
						order_set <- true
					}
					mutex.Unlock()
					wg.Done()
					done <- true
				} else {
					mutex.Lock()
					if bot.Side.String() == "buy" {
						if randomNumber < x {
							rate = ask_rate
							bot.setUserTrade(rate, minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
						} else {
							bot.setUserTrade(rate, minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
						}
					} else {
						if randomNumber < x {
							rate = bid_rate
							bot.setUserTrade(rate, major_amount, book_fee.FeeDecimal, book, oid, redis_client)
						} else {
							bot.setUserTrade(rate, major_amount, book_fee.FeeDecimal, book, oid, redis_client)
						}
					}
					mutex.Unlock()
					wg.Done()
					done <- true
				}
			}
		}()
		go func() {
			time_ticker := time.NewTicker(40 * time.Second)
			wg.Add(1)
			defer time_ticker.Stop()
			for range time_ticker.C {
				// Call your function here
				log.Println("1 minute has passed")
				bot.checkFunds()
				if bot.checkActiveOrders() && !bot.checkOrderStatus("completed", redis_client) {
					for _, oid := range bot.BitsoActiveOIds {
						if bot.Side.String() == "sell" {
							bot.checkOrderPrice(oid, bid_ticker, redis_client)
						} else {
							bot.checkOrderPrice(oid, ask_ticker, redis_client)
						}
					}
					bot.cancelOrder(bot.BitsoOId, redis_client)
				} else {
					log.Println("order was completed")
				}
				wg.Done()
			}
		}()
		go func() {
			kafkaData := <-kafka_avg_price // Receive the value from the channel

			// Create a zero-value instance of KafkaConsumer struct
			zeroKafkaData := queue.KafkaConsumer{}

			if kafkaData != zeroKafkaData {
				// Access the fields and perform further operations
				ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
				if err != nil {
					log.Fatalln("error getting ticket request")
				}
				ask_rate = ticker.Ask.Float64()
				bid_rate = ticker.Bid.Float64()
				w := newTabWriter()
				fmt.Fprintf(w, "WINDOW_START\tWINDOW_END\tNEXT_MINUTE\tAVG_BID\tSTD_DEV_BID\tAVG_ASK\tSTD_DEV_ASK\n")
				fmt.Fprintf(w, "%s\t%s\t%s\t%f\t%f\t%f\t%f\n",
					kafkaData.WindowStart,
					kafkaData.WindowEnd,
					kafkaData.NextMinute,
					kafkaData.AvgBid,
					kafkaData.BidStdDev,
					kafkaData.AvgAsk,
					kafkaData.AskStdDev,
				)
				w.Flush()
				w = newTabWriter()
				fmt.Fprintf(w, "CURRENT_TIME\tBID_RATE\tASK_RATE\n")
				fmt.Fprintf(w, "%s\t%f\t%f\n",
					time.Now(),
					bid_rate,
					ask_rate,
				)
				w.Flush()
				kafka_avg_price_task_done <- true
			} else {
				fmt.Println("kafkaData is empty or nil")
			}
		}()

		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("Method completed successfully, resume loop")
		case <-time.After(300 * time.Second):
			fmt.Println("Timed out")
			return
		case <-order_set:
			fmt.Println("Channel state changed to:", &order_set)
			wg.Wait()
			log.Println("all gorutines should now be completed")
		case <-kafka_avg_price_task_done:
			log.Println("Kafka avg prices done! Now check current prices with kafka data ")
		}
	}
}

func (bot *TradingBot) setBitsoClient() {
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	bot.BitsoClient.SetAPIKey(key)
	bot.BitsoClient.SetAPISecret(secret)
}

func (bot *TradingBot) setKafkaClient() {
	kafkaURL := os.Getenv("kafkaURL")
	topic := os.Getenv("topic")
	groupId := os.Getenv("groupID")
	kc := queue.NewKafkaClient(kafkaURL, topic, groupId)
	bot.KafkaClient = kc
}

func setBalance(total, locked float64, currency string) bitso.Balance {
	bcurrency := bitso.ToCurrency(currency)
	btotal := bitso.ToMonetary(total)
	blocked := bitso.ToMonetary(locked)
	available := bitso.ToMonetary(total - locked)
	balance := bitso.Balance{
		Currency:  bcurrency,
		Total:     btotal,
		Locked:    blocked,
		Available: available,
	}
	return balance
}

func (bot *TradingBot) getOptimalPrice(ticker *bitso.Ticker, market_trade, market_side string) float64 {
	var (
		amount     float64
		value      float64
		total      float64
		taker_fee  float64
		maker_fee  float64
		last_trade bitso.UserTrade
	)
	book := bot.BitsoBook
	lower_limit := 0.2 // MXN
	upper_limit := 0.1 // MXN
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("error while setting up redis db: ", err)
	}
	fee, err := redis_client.GetFee(book) // MXN
	if err != nil {
		log.Fatalln("error getting fee: ", err)
	}

	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	if len(bot.BitsoOId) == 0 {
		log.Println("Condition met")
		major_amount := bot.getMinMinorValue() / bid_rate
		minor_amount := bot.getMinMinorValue()
		if market_side == "sell" {
			amount = major_amount
			value = amount * bid_rate
			if market_trade == "taker" {
				taker_fee = value * fee.TakerFeeDecimal.Float64()
				total = (value + taker_fee + upper_limit) / amount // MXN
				// stop loss
				if bid_rate < (value - taker_fee - lower_limit) {
					total = value - lower_limit
				}
			} else {
				maker_fee = value * fee.MakerFeeDecimal.Float64()
				total = (value + maker_fee + upper_limit) / amount // MXN
				// stop loss
				if bid_rate < (value - maker_fee - lower_limit) {
					total = value - lower_limit
				}
			}
			ask_value := total * amount
			price_percentage_change := ((bid_rate / total) - 1) * 100
			w := newTabWriter()
			fmt.Fprintf(w, "AMOUNT(BTC)\tRATE\tVALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tRATE_CHANGE(%%)\n")
			fmt.Fprintf(w, "%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
				amount,
				bid_rate,
				ask_value,
				taker_fee,
				maker_fee,
				total,
				price_percentage_change,
			)
			w.Flush()
			if price_percentage_change > 0 {
				log.Printf("Price change is: %f%%", price_percentage_change)
				return bid_rate
			}
			return total
		} else {
			amount = minor_amount // MXN
			value = amount / ask_rate
			// upper_limit = upper_limit / ask_rate
			if market_trade == "taker" {
				taker_fee = value * fee.FeeDecimal.Float64()
				total = amount / (value + taker_fee) // BTC
			} else {
				maker_fee = value * fee.FeeDecimal.Float64()
				total = amount / (value + maker_fee) // BTC
			}
			bid_value := amount / total
			w := newTabWriter()
			price_percentage_change := ((ask_rate / total) - 1) * 100
			fmt.Fprintf(w, "AMOUNT(MXN)\tRATE\tVALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tRATE_CHANGE(%%)\n")
			fmt.Fprintf(w, "%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
				amount,
				ask_rate,
				bid_value,
				taker_fee,
				maker_fee,
				total,
				price_percentage_change,
			)
			w.Flush()
			if price_percentage_change < 0 {
				log.Printf("Price change is %f%", price_percentage_change)
				return ask_rate
			}
			return total
		}
	} else {
		log.Println("Condition not met")
		last_trade, err = redis_client.GetUserTrade(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error getting user last bid trade: ", err)
		}
		if market_side == "sell" {
			amount = last_trade.Major.Float64() - last_trade.FeesAmount.Float64() // BTC
			value = amount * bid_rate                                             // MXN
			change := -last_trade.Minor.Float64() - value                         // MXN
			if market_trade == "taker" {
				taker_fee = value * fee.FeeDecimal.Float64()
				total = (value + change + taker_fee + upper_limit) / amount // MXN
				// stop loss
				if bid_rate < (value - taker_fee - lower_limit) {
					total = value - lower_limit
				}
			} else {
				maker_fee = value * fee.FeeDecimal.Float64()
				total = (value + change + maker_fee + upper_limit) / amount // MXN
				// stop loss
				if bid_rate < (value - maker_fee - lower_limit) {
					total = value - lower_limit
				}
			}
			ask_value := total * amount
			price_percentage_change := ((bid_rate / total) - 1) * 100
			w := newTabWriter()
			fmt.Fprintf(w, "AMOUNT(BTC)\tRATE\tVALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tRATE_CHANGE(%%)\n")
			fmt.Fprintf(w, "%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
				amount,
				bid_rate,
				ask_value,
				taker_fee,
				maker_fee,
				total,
				price_percentage_change,
			)
			w.Flush()
			if price_percentage_change > 0 {
				log.Printf("Price change is: %f%%", price_percentage_change)
				return bid_rate
			}
			return total
		} else {
			amount = last_trade.Minor.Float64() - last_trade.FeesAmount.Float64() // MXN
			value = amount / ask_rate
			change := -last_trade.Major.Float64() - value
			// upper_limit = upper_limit / ask_rate
			if market_trade == "taker" {
				taker_fee = value * fee.FeeDecimal.Float64()
				total = amount / (value + change + taker_fee) // BTC
			} else {
				maker_fee = value * fee.FeeDecimal.Float64()
				total = amount / (value + change + maker_fee) // BTC
			}
			bid_value := amount / total
			w := newTabWriter()
			price_percentage_change := ((ask_rate / total) - 1) * 100
			fmt.Fprintf(w, "AMOUNT(MXN)\tRATE\tVALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tRATE_CHANGE(%%)\n")
			fmt.Fprintf(w, "%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
				amount,
				ask_rate,
				bid_value,
				taker_fee,
				maker_fee,
				total,
				price_percentage_change,
			)
			w.Flush()
			if price_percentage_change < 0 {
				log.Printf("Price change is %f%", price_percentage_change)
				return ask_rate
			}
			return total
		}
	}
}

func updateBalance(total, locked, available float64, currency bitso.Currency, redis_client *database.RedisClient) error {
	updatedBalance := &bitso.Balance{
		Currency:  currency,
		Total:     bitso.ToMonetary(total),
		Locked:    bitso.ToMonetary(locked),
		Available: bitso.ToMonetary(available),
	}
	err := redis_client.SaveUserBalance(updatedBalance)
	if err != nil {
		log.Fatalln("error updating user balance: ", err)
	}
	fmt.Println("Balance updated: ", updatedBalance)
	return nil
}

func (bot *TradingBot) setUserTrade(rate, amount float64, bitso_fee bitso.Monetary, book bitso.Book, oid string, redis_client *database.RedisClient) {
	var (
		value float64
		fee   float64
		total float64
	)
	created_at := bitso.Time(time.Now())
	tid := bitso.TID(time.Now().UnixNano())
	var user_trade *bitso.UserTrade
	major_balance := bot.MajorBalance
	minor_balance := bot.MinorBalance
	if bot.Side.String() == "sell" {
		value = amount * rate
		fee = value * bitso_fee.Float64()
		total = value - fee
		user_trade = &bitso.UserTrade{
			Book:         book,
			Major:        bitso.ToMonetary(-amount),
			CreatedAt:    created_at,
			Minor:        bitso.ToMonetary(value),
			FeesAmount:   bitso.ToMonetary(fee),
			FeesCurrency: book.Minor(),
			Price:        bitso.ToMonetary(rate),
			TID:          tid,
			OID:          oid,
			Side:         bot.Side,
		}
		major_locked := major_balance.Locked.Float64()
		major_available := major_balance.Available.Float64() - amount
		major_total_balance := major_locked + major_available
		err := updateBalance(major_total_balance, major_locked, major_available, book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		minor_locked := minor_balance.Locked.Float64()
		minor_available := minor_balance.Available.Float64() + total
		minor_total_balance := minor_locked + minor_available
		err = updateBalance(minor_total_balance, minor_locked, minor_available, book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.MajorBalance = setBalance(major_total_balance, major_locked, book.Major().String())
		bot.MinorBalance = setBalance(minor_total_balance, minor_locked, book.Minor().String())
		bot.Side = bitso.OrderSide(1)
		log.Println("Ask trade succesfully fulfilled!")
		err = bot.updateProfit(total, bot.BitsoBook.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating profit: ", err)
		}
	} else {
		value = amount / rate
		fee = value * bitso_fee.Float64()
		total = value - fee
		user_trade = &bitso.UserTrade{
			Book:         book,
			Major:        bitso.ToMonetary(value),
			CreatedAt:    created_at,
			Minor:        bitso.ToMonetary(-amount),
			FeesAmount:   bitso.ToMonetary(fee),
			FeesCurrency: book.Minor(),
			Price:        bitso.ToMonetary(rate),
			TID:          tid,
			OID:          oid,
			Side:         bot.Side,
		}
		major_locked := major_balance.Locked.Float64()
		major_available := major_balance.Available.Float64() + total
		major_total_balance := major_locked + major_available
		err := updateBalance(major_total_balance, major_locked, major_available, book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		minor_locked := minor_balance.Locked.Float64()
		minor_available := minor_balance.Available.Float64() - amount
		minor_total_balance := minor_locked + minor_available
		err = updateBalance(minor_total_balance, minor_locked, minor_available, book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.MajorBalance = setBalance(major_total_balance, major_locked, book.Major().String())
		bot.MinorBalance = setBalance(minor_total_balance, minor_locked, book.Minor().String())
		bot.Side = bitso.OrderSide(2)
		log.Println("Bid trade succesfully fulfilled!")
		err = bot.updateProfit(total, bot.BitsoBook.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating profit: ", err)
		}
	}
	err := redis_client.SaveTrade(user_trade)
	if err != nil {
		log.Fatalln("error during getting trading amount: ", err)
	}
	bot.BitsoOId = oid
}

func (bot *TradingBot) setOrder(ticker *bitso.Ticker, amount, rate float64, order_type bitso.OrderType, redis_client *database.RedisClient) {
	var order *bitso.OrderPlacement
	if !bot.Debug {
		if bot.Side.String() == "sell" {
			order = &bitso.OrderPlacement{
				Book:  bot.BitsoBook,
				Side:  bot.Side,
				Type:  order_type,
				Major: "",
				Minor: bitso.ToMonetary(amount),
				Price: bitso.ToMonetary(rate),
			}
			oid, err := bot.BitsoClient.PlaceOrder(order)
			if err != nil {
				log.Fatalln("Error placing order: ", err)
			}
			bot.BitsoOId = oid
			bot.Side = bitso.OrderSide(1)
		} else {
			order = &bitso.OrderPlacement{
				Book:  bot.BitsoBook,
				Side:  bot.Side,
				Type:  order_type,
				Major: bitso.ToMonetary(amount),
				Minor: "",
				Price: bitso.ToMonetary(rate),
			}
			log.Println("final order details: ", order)
			log.Println("amount as minor: ", amount*rate)
			oid, err := bot.BitsoClient.PlaceOrder(order)
			if err != nil {
				log.Fatalln("error placing order: ", err)
			}
			bot.BitsoOId = oid
			bot.Side = bitso.OrderSide(2)
		}
		balances, err := bot.BitsoClient.Balances(nil)
		if err != nil {
			log.Fatalln("error during balances request:", err)
		}
		for _, balance := range balances {
			if balance.Currency == bot.BitsoBook.Major() {
				bot.MajorBalance = balance
			} else if balance.Currency == bot.BitsoBook.Minor() {
				bot.MinorBalance = balance
			}
		}
	} else {
		oid := utils.GenRandomOId(7)
		order_status := bitso.OrderStatus(1)
		if bot.Side.String() == "sell" {
			user_order := &bitso.UserOrder{
				Book:           bot.BitsoBook,
				OriginalAmount: bitso.ToMonetary(amount),
				UnfilledAmount: bitso.ToMonetary(amount),
				OriginalValue:  bitso.ToMonetary(amount * rate),
				CreatedAt:      bitso.Time(time.Now()),
				UpdatedAt:      bitso.Time(time.Now()),
				Price:          bitso.ToMonetary(rate),
				OID:            oid,
				Side:           bot.Side,
				Status:         order_status,
				Type:           order_type.String(),
			}
			log.Println("final order details: ", order)
			log.Println("amount as minor: ", amount*rate)
			redis_client.SaveUserOrder(*user_order)
			bot.BitsoOId = oid
			bot.Side = bitso.OrderSide(2)
		} else {
			user_order := &bitso.UserOrder{
				Book:           bot.BitsoBook,
				OriginalAmount: bitso.ToMonetary(amount / rate),
				UnfilledAmount: bitso.ToMonetary(amount / rate),
				OriginalValue:  bitso.ToMonetary(amount),
				CreatedAt:      bitso.Time(time.Now()),
				UpdatedAt:      bitso.Time(time.Now()),
				Price:          bitso.ToMonetary(rate),
				OID:            oid,
				Side:           bot.Side,
				Status:         order_status,
				Type:           order_type.String(),
			}
			log.Println("final order details: ", order)
			log.Println("amount as minor: ", amount*rate)
			redis_client.SaveUserOrder(*user_order)
			bot.BitsoOId = oid
			bot.Side = bitso.OrderSide(2)
		}
		balances, err := bot.BitsoClient.Balances(nil)
		if err != nil {
			log.Fatalln("error during balances request:", err)
		}
		for _, balance := range balances {
			if balance.Currency == bot.BitsoBook.Major() {
				bot.MajorBalance = balance
			} else if balance.Currency == bot.BitsoBook.Minor() {
				bot.MinorBalance = balance
			}
		}

	}
	log.Println("Place Order request successfully fulfill")
}

func (bot *TradingBot) updatePublicRequestPerMinute() {
	bot.MaxPrivateRequestPerMinute--
}

func (bot *TradingBot) updatePrivateRequestPerMinute() {
	bot.MaxPrivateRequestPerMinute--
}
func (bot *TradingBot) resetPublicRequestPerMinute() {
	bot.MaxPrivateRequestPerMinute = 60
}

func (bot *TradingBot) resetPrivateRequestPerMinute() {
	bot.MaxPrivateRequestPerMinute = 100
}

func (bot *TradingBot) checkFunds() {
	if bot.Side.String() == "sell" && bot.MajorBalance.Available.Float64() <= 0 {
		log.Fatalf("No %s funds available to sell", bot.MajorBalance.Currency.String())
	}
	if bot.Side.String() == "buy" && bot.MinorBalance.Available.Float64() <= 0 {
		log.Fatalf("No %s funds available to buy", bot.MinorBalance.Currency.String())
	}
}

func (bot *TradingBot) updateProfit(total float64, currency bitso.Currency, redis_client *database.RedisClient) error {
	if currency == bot.BitsoBook.Major() {
		if bot.MajorProfit.Initial == 0 {
			bot.MajorProfit.Initial = total
			bot.MajorProfit.Currency = currency
			bot.MajorProfit.Last = 0.0
			bot.MajorProfit.Cumulative = 0.0
			bot.MajorProfit.Change = 0.0
			bot.MajorProfit.CumulativeChange = 0.0
			bot.MajorProfit.UpdatedAt = time.Now()
		} else {
			initial := bot.MajorProfit.Initial
			last := total
			cum := total - initial // 10 -100 = - 90
			change := ((total - bot.MajorProfit.Last) / bot.MajorProfit.Last) * 100
			cumchange := (cum / initial) * 100
			update := time.Now()
			bot.MajorProfit.Last = last
			bot.MajorProfit.Cumulative = cum
			bot.MajorProfit.Change = change
			bot.MajorProfit.CumulativeChange = cumchange
			bot.MajorProfit.UpdatedAt = update
		}
		w := newTabWriter()
		fmt.Fprintf(w, "CURRENCY\tINITIAL\tLAST\tCUM\tCHANGE(%%)\tCUM_CHANGE(%%)\tUPDATE\n")
		fmt.Fprintf(w, "%v\t%f\t%f\t%f\t%f\t%f\t%v\n",
			bot.MajorProfit.Currency,
			bot.MajorProfit.Initial,
			bot.MajorProfit.Last,
			bot.MajorProfit.Cumulative,
			bot.MajorProfit.Change,
			bot.MajorProfit.CumulativeChange,
			bot.MajorProfit.UpdatedAt,
		)
		w.Flush()
	} else {
		if bot.MinorProfit.Initial == 0 {
			bot.MinorProfit.Initial = total
			bot.MinorProfit.Currency = currency
			bot.MinorProfit.Last = 0.0
			bot.MinorProfit.Cumulative = 0.0
			bot.MinorProfit.Change = 0.0
			bot.MinorProfit.CumulativeChange = 0.0
			bot.MinorProfit.UpdatedAt = time.Now()
		} else {
			initial := bot.MinorProfit.Initial
			last := total
			cum := total - initial // 10 -100 = - 90
			change := ((total - bot.MinorProfit.Last) / bot.MinorProfit.Last) * 100
			cumchange := (cum / initial) * 100
			update := time.Now()
			bot.MinorProfit.Last = last
			bot.MinorProfit.Cumulative = cum
			bot.MinorProfit.Change = change
			bot.MinorProfit.CumulativeChange = cumchange
			bot.MinorProfit.UpdatedAt = update

		}
		w := newTabWriter()
		fmt.Fprintf(w, "CURRENCY\tINITIAL\tLAST\tCUM\tCHANGE(%%)\tCUM_CHANGE(%%)\tUPDATE\n")
		fmt.Fprintf(w, "%v\t%f\t%f\t%f\t%f\t%f\t%v\n",
			bot.MinorProfit.Currency,
			bot.MinorProfit.Initial,
			bot.MinorProfit.Last,
			bot.MinorProfit.Cumulative,
			bot.MinorProfit.Change,
			bot.MinorProfit.CumulativeChange,
			bot.MinorProfit.UpdatedAt,
		)
		w.Flush()
	}
	// err := redis_client.SaveUserProfit(bot.Profit)
	// if err != nil {
	// 	log.Fatalln("error updating user profit: ", err)
	// }
	return nil
}

func (bot *TradingBot) cancelOrder(oid string, redis_client *database.RedisClient) {
	if !bot.Debug {
		open_orders, err := bot.BitsoClient.MyOpenOrders(nil)
		if err != nil {
			log.Fatalln("error during open orders request: ", err)
		}
		for _, open_order := range open_orders {
			if open_order.OID == bot.BitsoOId {
				if open_order.Status == bitso.OrderStatus(1) {
					res, err := bot.BitsoClient.CancelOrder(oid)
					if err != nil {
						log.Fatalln("error during cancel order request: ", err)
					}
					log.Println("cancel order request successfully fulfill: ", res)
				}
			}
		}
	} else {
		user_order, err := redis_client.GetUserOrderById(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error getting user_order: ", err)
		}
		if len(user_order.OID) == 0 {
			log.Fatalln("user_order is empty: ", err)
		}
		user_order.Status = bitso.OrderStatus(4)
		err = redis_client.SaveUserOrder(user_order)
		if err != nil {
			log.Fatalln("error getting user_order!")
		}
		arr := make([]string, 0)
		for _, active_order := range bot.BitsoActiveOIds {
			if active_order != oid {
				arr = append(arr, active_order)
			}
		}
		bot.BitsoActiveOIds = arr
	}
	log.Println("Order cancelation successfully fulfill")
}

func (bot *TradingBot) cancelOrders(oids []string) {
}

func (bot *TradingBot) getRate(redis_client *database.RedisClient) bitso.Monetary {
	ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook)
	if err != nil {
		log.Fatalln("error getting ticket request: ", err)
	}
	if bot.Side.String() == "sell" {
		return ticker.Ask
	} else {
		return ticker.Bid
	}
}

func (bot *TradingBot) getMinMinorValue() float64 {
	eo_book := bot.BitsoExchangeBook
	minor_amount := 12.0
	minor_amount = bot.MinorBalance.Available.Float64()
	if minor_amount > eo_book.MinimumValue.Float64() {
		return eo_book.MinimumValue.Float64()
	}
	return minor_amount
}

func (bot *TradingBot) getMinMajorAmount() float64 {
	eo_book := bot.BitsoExchangeBook
	// major_amount := 0.000075
	major_amount := bot.MajorBalance.Available.Float64()
	if major_amount > eo_book.MinimumAmount.Float64() {
		return eo_book.MinimumAmount.Float64()
	}
	return major_amount
}

/*
	minimum_price	The minimum price when placing an order.	String	Minor
	maximum_price	The maximum price when placing an order.	String	Minor
*/
func (bot *TradingBot) checkPrice(rate float64, redis_client *database.RedisClient) bool {
	exchange_order_book, err := redis_client.GetExchangeOrderBook(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting exchange order book: ", err)
	}
	if rate < exchange_order_book.MinimumPrice.Float64() {
		log.Fatalln("Error price below minimum: ", err)
	} else if rate > exchange_order_book.MaximumPrice.Float64() {
		log.Fatalln("Error price above maximum: ", err)
	}
	return true
}

/*
	minimum_amount	The minimum amount of major when placing an order.	String	Major
	maximum_amount	The maximum amount of major when placing an order.	String	Major
*/
func (bot *TradingBot) checkAmount(amount float64, redis_client *database.RedisClient) bool {
	exchange_order_book, err := redis_client.GetExchangeOrderBook(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting exchange order book: ", err)
	}
	if amount < exchange_order_book.MinimumAmount.Float64() {
		log.Fatalln("Error amount below minimum: ", err)
	} else if amount > exchange_order_book.MaximumAmount.Float64() {
		log.Fatalln("Error amount above maximum: ", err)
	}
	return true
}

/*
	minimum_value	The minimum value amount (amount*price) when placing an order.	String	Minor
	maximum_value	The maximum value amount (amount*price) when placing an order.	String	Minor
*/
func (bot *TradingBot) checkValue(value float64, redis_client *database.RedisClient) bool {
	exchange_order_book, err := redis_client.GetExchangeOrderBook(bot.BitsoBook)
	if err != nil {
		log.Fatalln("error getting exchange order book: ", err)
	}
	if value < exchange_order_book.MinimumValue.Float64() {
		log.Println("value below minimum: ", err)
		return false
	} else if value > exchange_order_book.MaximumValue.Float64() {
		log.Println("value above maximum: ", err)
		return false
	}
	return true
}

func (bot *TradingBot) getOptimalProfitFactor(amount float64) float64 {
	var condition bool
	var factor float64
	switch condition {
	case (amount > 1 && amount <= 50):
		factor = 1
	case (amount > 50 && amount <= 100):
		factor = 1.5
	default:
		factor = 3
	}
	return factor
}

func (bot *TradingBot) checkActiveOrders() bool {
	return len(bot.BitsoActiveOIds) == 0
}

func (bot *TradingBot) checkOrderStatus(order_status string, redis_client *database.RedisClient) bool {
	if len(bot.BitsoOId) == 0 {
		return false
	}
	if !bot.Debug {
		user_order, err := bot.BitsoClient.LookupOrder(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error lookin up user order", err)
		}
		if user_order.Status.String() == order_status {
			return true
		}
	} else {
		user_order, err := redis_client.GetUserOrderById(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error getting user_order: ", err)
		}
		if len(user_order.OID) == 0 {
			log.Fatalln("user_order is empty!")
		}
		if user_order.Status.String() == order_status {
			return true
		}
	}
	return false
}

func (bot *TradingBot) checkOrderPrice(oid string, trend int32, ticker *bitso.Ticker, redis_client *database.RedisClient) {
	order_type := bitso.OrderType(2) // limit
	if !bot.Debug {
		user_order, err := bot.BitsoClient.LookupOrder(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error lookin up user order", err)
		}
		if user_order.Side.String() == "sell" {
			bid_rate := ticker.Ask.Float64()
			rate := bid_rate
			amount := user_order.OriginalAmount.Float64()
			if trend > 0 {
				if ticker.Bid.Float64() > user_order.Price.Float64() {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, rate, order_type, redis_client)
				}

			}
		} else {
			ask_rate := ticker.Ask.Float64()
			rate := user_order.Price.Float64() / ask_rate
			amount := user_order.OriginalValue.Float64()
			if trend <= 0 {
				if ask_rate < rate {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, ticker.Bid.Float64(), order_type, redis_client)
				}

			}
		}
	} else {
		user_order, err := redis_client.GetUserOrderById(bot.BitsoOId)
		if err != nil {
			log.Fatalln("error getting user order", err)
		}
		if user_order.Side.String() == "sell" {
			bid_rate := ticker.Ask.Float64()
			rate := bid_rate
			amount := user_order.OriginalAmount.Float64()
			if trend > 0 {
				if ticker.Bid.Float64() > user_order.Price.Float64() {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, rate, order_type, redis_client)
				}

			}
		} else {
			ask_rate := ticker.Ask.Float64()
			rate := user_order.Price.Float64() / ask_rate
			amount := user_order.OriginalValue.Float64()
			if trend <= 0 {
				if ask_rate < rate {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, ticker.Bid.Float64(), order_type, redis_client)
				}

			}
		}
	}
}

func (bot *TradingBot) KafkaPriceTrendConsumer(kafka_ch chan queue.KafkaConsumer, k_wg *sync.WaitGroup) {
	var reader queue.KafkaConsumer
	defer k_wg.Done()
	fmt.Println("start consuming ... !!")
	for {
		m, err := bot.KafkaClient.Reader.ReadMessage(context.Background())
		if err != nil {
			log.Fatalln("error reading message for kafka: ", err)
		}
		// fmt.Printf("message at topic: %v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
		err = json.Unmarshal(m.Value, &reader)
		if err != nil {
			log.Println("error while unmarshaled message: ", err)
		}
		kafka_ch <- reader
		// if len(order.Book.Minor().String()) > 0 {
		// 	err = controllers.ProfitHandler(&order, client, clientFees, clientBalances)
		// 	if err != nil {
		// 		log.Fatalln(err)
		// 	}
		// 	utils.Rest(5)
		// }
	}
}

func (bot *TradingBot) KafkaAvgPricesConsumer(kafka_ch chan queue.KafkaConsumer, k_wg *sync.WaitGroup) {
	var reader queue.KafkaConsumer
	// defer k_wg.Done()
	time_ticker := time.NewTicker(3 * time.Second)
	defer time_ticker.Stop()
	fmt.Println("start consuming ... !!")
	for {
		k_wg.Add(1)
		select {
		case <-time_ticker.C:
			// Perform your periodic actions here
			log.Println("1 minute has passed in Kafka Msg Service")
			m, err := bot.KafkaClient.Reader.ReadMessage(context.Background())
			if err != nil {
				log.Fatalln("error reading message for kafka: ", err)
			}
			fmt.Printf("message at topic: %v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
			tmp_parser := struct {
				Window      queue.Window `json:"window"`
				WindowStart string       `json:"window_start"`
				WindowEnd   string       `json:"window_end"`
				NextMinute  string       `json:"next_minute"`
				AvgBid      float64      `json:"avg_bid"`
				BidStdDev   float64      `json:"bid_stddev"`
				AvgAsk      float64      `json:"avg_ask"`
				AskStdDev   float64      `json:"ask_stddev"`
			}{}
			err = json.Unmarshal(m.Value, &tmp_parser)
			if err != nil {
				log.Println("error while unmarshaled message: ", err)
			}
			const timeFormat = "2006-01-02T15:04:05.000-07"
			const nextMinute_timeFormat = "2006-01-02T15:04:05.000-07"
			windowStart, err := time.Parse(timeFormat, tmp_parser.WindowStart)
			if err != nil {
				log.Fatalf("error parsing WindowStart: %v", err)
			}

			windowEnd, err := time.Parse(timeFormat, tmp_parser.WindowEnd)
			if err != nil {
				log.Fatalf("error parsing WindowEnd: %v", err)
			}

			nextMinute, err := time.Parse(nextMinute_timeFormat, tmp_parser.NextMinute)
			if err != nil {
				log.Fatalf("error parsing NextMinute: %v", err)
			}
			// Subtract 1 hour from the time values
			windowStart = windowStart.Add(-time.Hour)
			windowEnd = windowEnd.Add(-time.Hour)
			nextMinute = nextMinute.Add(-time.Hour)

			reader.Window = tmp_parser.Window
			reader.WindowStart = windowStart
			reader.WindowEnd = windowEnd
			reader.NextMinute = nextMinute
			reader.AvgBid = tmp_parser.AvgBid
			reader.BidStdDev = tmp_parser.BidStdDev
			reader.AvgAsk = tmp_parser.AvgAsk
			reader.AskStdDev = tmp_parser.AskStdDev
			k_wg.Done()
			kafka_ch <- reader
		}
	}
}

func stopSimulation() {
	log.Fatalln("Simulation stopped")
}
