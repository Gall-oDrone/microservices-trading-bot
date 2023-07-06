package simulation

import (
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"text/tabwriter"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
	"github.com/xiam/bitso-go/bitso"
)

type TradingBot struct {
	BitsoClient                *bitso.Client
	DBClient                   interface{}
	BitsoBook                  bitso.Book
	BitsoOId                   string
	MajorBalance               bitso.Balance
	MinorBalance               bitso.Balance
	Side                       bitso.OrderSide
	MaxPublicRequestPerMinute  int8
	MaxPrivateRequestPerMinute int8
	Debug                      bool
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
	} else {
		bot.MinorBalance = setBalance(1000.0, 0.0, "MXN")
	}
	err = redis_client.SaveUserBalance(&bot.MinorBalance)
	if err != nil {
		log.Fatal("Error saving user balance: ", err)
	}

	fees, err := bot.BitsoClient.Fees(nil)
	if err != nil {
		log.Fatalln("Error during Fees request: ", err)
	}

	err = redis_client.SaveFees(fees.Fees)
	if err != nil {
		log.Fatalln("Error saving fees: ", err)
	}

	book_fee, err := redis_client.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting book fee: ", err)
	}

	ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error during ticker request: ", err)
	}

	rate := ticker.Ask.Float64()
	amount := bot.MinorBalance.Available.Float64()
	bitso_fee := book_fee.FeeDecimal
	locked := 0.0
	oid := utils.GenRandomOId(7)

	bot.setUserTrade(rate, amount, locked, bitso_fee, bot.BitsoBook, oid, bot.MinorBalance, bot.Side, redis_client)
}

func (bot *TradingBot) generateAskTrade() {
	var (
		amount float64
		rate   float64
		value  float64
		fee    float64
		total  float64
		locked float64
	)
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("Failed to connect to Redis:", err)
	}
	defer redis_client.CloseDB()

	book := bot.BitsoBook
	currency := book.Major().String()
	threshold := 0.05
	lower_limit := 2.0
	upper_limit := 3.0
	market_trade := "taker"
	balance, err := redis_client.GetUserBalance(currency)
	if err != nil {
		log.Fatalln("error getting user balance: ", err)
	}
	if balance.Total.Float64() <= 0.0 || balance.Available.Float64() <= 0.0 {
		log.Fatalln("error user balance is 0: ", err)
	}
	book_fee, err := redis_client.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting book fee: ", err)
	}
	locked = balance.Locked.Float64()
	amount = balance.Available.Float64()
	ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
	if err != nil {
		log.Fatalln("error getting ticket request")
	}
	rate = bot.getOptimalPrice(ticker, threshold, market_trade, bot.Side.String())
	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	done := make(chan bool)
	ask_trade := make(chan bool)
	bid_trade := make(chan bool)
	sleep := (10 * time.Second)
	log.Println("Start")
	for {
		go func() {
			if bot.Side.String() == "sell" {
				bot.checkFunds()
				params := url.Values{}
				params.Set("book", book.String())
				orders, err := bot.BitsoClient.OrderBook(params)
				if err != nil {
					log.Fatalln("Error during open order request: ", err)
				}
				w := newTabWriter()
				for _, order := range orders.Bids {
					fmt.Fprintf(w, "BOOK\tORDER_AMOUNT\tAVAILABLE\tORDER_PRICE\tPROFIT_PRICE\tSIDE\n")
					fmt.Fprintf(w, "%s\t%f\t%f\t%f\t%f\t%s\n",
						order.Book.String(),
						order.Amount.Float64(),
						balance.Available.Float64(),
						order.Price.Float64(),
						rate,
						bot.Side.String(),
					)
					w.Flush()
					if order.OID != bot.BitsoOId && order.Amount.Float64() == amount && order.Price.Float64() >= rate {
						order_rate := order.Price
						value = amount * order_rate.Float64()
						fee = value * book_fee.FeeDecimal.Float64()
						total = value - fee
						oid := order.OID
						order_type := bitso.OrderType(2)
						if !bot.Debug {
							bot.setOrder(ticker, order_rate.Float64(), order_type, threshold, market_trade, total, amount, locked, book, book_fee, bot.Side, redis_client)
						} else {
							bot.setUserTrade(rate, amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						}
						log.Panicln("Ask trade successfully fulfill")
						ask_trade <- true
					}
				}
				log.Println("Now sleeping")
				time.Sleep(sleep)
				log.Println("Now waking")
				done <- true
			}
		}()
		go func() {
			if bot.Side.String() == "buy" {
				bot.checkFunds()
				params := url.Values{}
				params.Set("book", book.String())
				orders, err := bot.BitsoClient.OrderBook(params)
				if err != nil {
					log.Fatalln("Error during open order request: ", err)
				}
				w := newTabWriter()
				for _, order := range orders.Asks {
					fmt.Fprintf(w, "BOOK\tORDER_AMOUNT\tAVAILABLE\tORDER_PRICE\tPROFIT_PRICE\tSIDE\n")
					fmt.Fprintf(w, "%s\t%f\t%f\t%f\t%f\t%s\n",
						order.Book.String(),
						order.Amount.Float64(),
						balance.Available.Float64(),
						order.Price.Float64(),
						rate,
						bot.Side.String(),
					)
					w.Flush()
					if order.OID != bot.BitsoOId && order.Amount.Float64() == amount && order.Price.Float64() >= rate {
						order_rate := order.Price
						value = amount * order_rate.Float64()
						fee = value * book_fee.FeeDecimal.Float64()
						total = value - fee
						oid := order.OID
						if !bot.Debug {
							bot.setUserTrade(order_rate.Float64(), amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						} else {
							bot.setUserTrade(rate, amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						}
						log.Panicln("Buy trade successfully fulfill")
						bid_trade <- true
					}
				}
				log.Println("Now sleeping")
				time.Sleep(sleep)
				log.Println("Now waking")
				done <- true
			}
		}()
		go func() {
			time_ticker := time.NewTicker(30 * time.Minute)
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
				minor_amount := bot.MinorBalance.Available.Float64()
				major_amount := bot.MajorBalance.Available.Float64()
				market_trade = "maker"
				order_type := bitso.OrderType(2)
				x := 0.5
				rand.Seed(time.Now().UnixNano())
				randomNumber := rand.Float64()
				if !bot.Debug {
					if bot.Side.String() == "buy" {
						rate = bot.getOptimalPrice(ticker, threshold, market_trade, bot.Side.String())
						bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, minor_amount, locked, book, book_fee, bot.Side, redis_client)
					} else {
						rate = bot.getOptimalPrice(ticker, threshold, market_trade, bot.Side.String())
						bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, major_amount, locked, book, book_fee, bot.Side, redis_client)
					}
				} else {
					fmt.Println("the random number is: ", randomNumber)
					if bot.Side.String() == "buy" {
						if randomNumber < x {
							rate = ask_rate //((ask_rate * amount) - lower_limit)
							value = minor_amount / rate
							fee = value * book_fee.FeeDecimal.Float64()
							total = value - fee
							oid := utils.GenRandomOId(7)
							bot.setUserTrade(rate, minor_amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						} else {
							value = minor_amount / rate
							fee = value * book_fee.FeeDecimal.Float64()
							oid := utils.GenRandomOId(7)
							bot.setUserTrade(rate, minor_amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						}
					} else {
						if randomNumber < x {
							rate := bid_rate //((ask_rate * amount) - lower_limit)
							value = major_amount * rate
							fee = value * book_fee.FeeDecimal.Float64()
							oid := utils.GenRandomOId(7)
							bot.setUserTrade(rate, major_amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						} else {
							value = major_amount * rate
							fee = value * book_fee.FeeDecimal.Float64()
							oid := utils.GenRandomOId(7)
							bot.setUserTrade(rate, major_amount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
						}
					}
				}
			}
		}()
		go func() {
			// Stop loss/ take profit
			time_ticker := time.NewTicker(30 * time.Second)
			defer time_ticker.Stop()

			for range time_ticker.C {
				// Call your function here
				log.Println("1 minute has passed")
				bot.checkFunds()
				x := 0.5
				rand.Seed(time.Now().UnixNano())
				randomNumber := rand.Float64()
				fmt.Println("the random number is: ", randomNumber)
				minoramount := bot.MinorBalance.Available.Float64()
				majoramount := bot.MajorBalance.Available.Float64()
				ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
				if err != nil {
					log.Fatalln("error getting ticket request")
				}
				current_ask_rate := ticker.Ask.Float64()
				current_bid_rate := ticker.Bid.Float64()
				if bot.Side.String() == "sell" {
					value := bid_rate * majoramount
					c_value := current_bid_rate * majoramount
					fee = value * book_fee.FeeDecimal.Float64()
					c_fee := c_value * book_fee.FeeDecimal.Float64()
					if (c_value - c_fee) <= (value - fee - lower_limit) {
						// Stop loss
						if !bot.Debug {
							order_type := bitso.OrderType(2)
							market_trade = "maker"
							rate := (current_ask_rate * majoramount) - lower_limit
							bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, majoramount, locked, bot.BitsoBook, book_fee, bot.Side, redis_client)
						} else {
							if randomNumber < x {
								rate := current_bid_rate //((ask_rate * amount) - lower_limit)
								value = majoramount * rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = (value - fee) / majoramount
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, majoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							} else {
								rate := current_ask_rate //((ask_rate * amount) - lower_limit)
								value = majoramount * rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = (value - fee - lower_limit) / majoramount
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, majoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)

							}

						}
					} else {
						// Take profit
						if !bot.Debug {
							order_type := bitso.OrderType(2)
							market_trade = "maker"
							rate := current_bid_rate + upper_limit
							bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, majoramount, locked, bot.BitsoBook, book_fee, bot.Side, redis_client)
						} else {
							if randomNumber < x {
								// Worse Case
								rate := current_ask_rate //((ask_rate * amount) - lower_limit)
								value = majoramount * rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = (value - fee + upper_limit) / majoramount
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, majoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							} else {
								// Best Case
								rate := current_bid_rate //((ask_rate * amount) - lower_limit)
								value = majoramount * rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = (value + fee + upper_limit) / majoramount
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, majoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							}

						}
					}
				} else {
					value := minoramount / ask_rate
					c_value := minoramount / current_ask_rate
					fee = value * book_fee.FeeDecimal.Float64()
					c_fee := c_value * book_fee.FeeDecimal.Float64()
					lower_limit = lower_limit / ask_rate
					if (c_value - c_fee) < (value - fee - lower_limit) {
						// Take profit
						if !bot.Debug {
							order_type := bitso.OrderType(2)
							market_trade = "maker"
							fee = c_fee * current_ask_rate
							rate := minoramount / current_ask_rate
							bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, minoramount, locked, bot.BitsoBook, book_fee, bot.Side, redis_client)
						} else {
							if randomNumber < x {
								rate := current_ask_rate
								value = minoramount / rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = minoramount / (value - fee - lower_limit)
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, minoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							} else {
								rate := current_bid_rate
								value = minoramount / rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = minoramount / (value - fee - lower_limit)
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, minoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							}
						}
					} else {
						// Stop loss
						if !bot.Debug {
							order_type := bitso.OrderType(2)
							market_trade = "maker"
							fee = fee * ask_rate
							rate := minoramount / (value - lower_limit)
							bot.setOrder(ticker, rate, order_type, threshold, market_trade, total, minoramount, locked, bot.BitsoBook, book_fee, bot.Side, redis_client)
						} else {
							if randomNumber < x {
								rate := ask_rate
								value = minoramount / rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = minoramount / (value - fee - lower_limit)
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, minoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							} else {
								rate := bid_rate
								value = minoramount / rate
								fee = value * book_fee.FeeDecimal.Float64()
								rate = minoramount / (value - fee - lower_limit)
								oid := utils.GenRandomOId(7)
								bot.setUserTrade(rate, minoramount, locked, book_fee.FeeDecimal, book, oid, balance, bot.Side, redis_client)
							}
						}
					}
				}
			}
		}()

		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("Method completed successfully, resume loop")
		case <-time.After(300 * time.Second):
			fmt.Println("Timed out")
			return
		}
	}
}

func (bot *TradingBot) setBitsoClient() {
	key := os.Getenv("BITSO_API_KEY")
	secret := os.Getenv("BITSO_API_SECRET")
	bot.BitsoClient.SetAPIKey(key)
	bot.BitsoClient.SetAPISecret(secret)
}

func setBalance(total, locked float64, currency string) bitso.Balance {
	f_available := total - locked
	bcurrency := bitso.ToCurrency(currency)
	btotal := bitso.ToMonetary(total)
	blocked := bitso.ToMonetary(locked)
	available := bitso.ToMonetary(f_available)
	balance := bitso.Balance{
		Currency:  bcurrency,
		Total:     btotal,
		Locked:    blocked,
		Available: available,
	}
	return balance
}

func (bot *TradingBot) getOptimalPrice(ticker *bitso.Ticker, threshold float64, market_trade, market_side string) float64 {
	var amount float64
	var value float64
	var total float64
	var taker_fee float64
	var maker_fee float64
	book := bot.BitsoBook
	lower_limit := 2.0 // MXN
	upper_limit := 3.0 // MXN
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("error while setting up redis db: ", err)
	}
	last_trade, err := redis_client.GetUserTrade(bot.BitsoOId)
	if err != nil {
		log.Fatalln("error getting user last bid trade: ", err)
	}
	fee, err := redis_client.GetFee(book) // MXN
	if err != nil {
		log.Fatalln("error getting fee: ", err)
	}

	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	if market_side == "sell" {
		amount = last_trade.Major.Float64() - last_trade.FeesAmount.Float64() // BTC
		value = amount * bid_rate
		change := -last_trade.Minor.Float64() - value
		if market_trade == "taker" {
			taker_fee = (value) * fee.FeeDecimal.Float64()
			total = (value + change + taker_fee + upper_limit) / amount // MXN
			// stop loss
			if bid_rate < (value - taker_fee - lower_limit) {
				total = value - lower_limit
			}
		} else {
			maker_fee = (value) * fee.FeeDecimal.Float64()
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
			log.Printf("Price change is: %f%", price_percentage_change)
			return ask_rate
		}
		return total
	} else {
		amount = last_trade.Minor.Float64() - last_trade.FeesAmount.Float64() // MXN
		value = amount / ask_rate
		change := -last_trade.Major.Float64() - value
		upper_limit = upper_limit / ask_rate
		if market_trade == "taker" {
			taker_fee = value * fee.FeeDecimal.Float64()
			total = amount / (value + change + taker_fee + upper_limit) // BTC
		} else {
			maker_fee = value * fee.FeeDecimal.Float64()
			total = amount / (value + change + maker_fee + upper_limit) // BTC
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
			return bid_rate
		}
		return total
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

func (bot *TradingBot) setUserTrade(rate, amount, locked float64, bitso_fee bitso.Monetary, book bitso.Book, oid string, balance bitso.Balance, side bitso.OrderSide, redis_client *database.RedisClient) {
	var (
		value float64
		fee   float64
		total float64
	)
	created_at := bitso.Time(time.Now())
	tid := bitso.TID(time.Now().UnixNano())
	var user_trade *bitso.UserTrade
	if side.String() == "sell" {
		value = amount * rate
		fee = value * bitso_fee.Float64()
		total = value - fee
		user_trade = &bitso.UserTrade{
			Book:         book,
			Major:        bitso.ToMonetary(-balance.Available.Float64()),
			CreatedAt:    created_at,
			Minor:        bitso.ToMonetary(value),
			FeesAmount:   bitso.ToMonetary(fee),
			FeesCurrency: book.Minor(),
			Price:        bitso.ToMonetary(rate),
			TID:          tid,
			OID:          oid,
			Side:         side,
		}
		err := updateBalance(0.0, locked, 0.0, book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		err = updateBalance(total, 0.0, (total - locked), book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.MajorBalance = setBalance(0.0, locked, book.Major().String())
		bot.MinorBalance = setBalance(total, locked, book.Minor().String())
		bot.Side = bitso.OrderSide(1)
		log.Println("Ask trade succesfully fulfilled!")
	} else {
		value = amount / rate
		fee = value * bitso_fee.Float64()
		total = value - fee
		user_trade = &bitso.UserTrade{
			Book:         book,
			Major:        bitso.ToMonetary(value),
			CreatedAt:    created_at,
			Minor:        bitso.ToMonetary(-balance.Available.Float64()),
			FeesAmount:   bitso.ToMonetary(fee),
			FeesCurrency: book.Minor(),
			Price:        bitso.ToMonetary(rate),
			TID:          tid,
			OID:          oid,
			Side:         side,
		}
		err := updateBalance(total, 0.0, (total - locked), book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		err = updateBalance(0.0, locked, 0.0, book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.MajorBalance = setBalance(total, locked, book.Major().String())
		bot.MinorBalance = setBalance(0.0, locked, book.Minor().String())
		bot.Side = bitso.OrderSide(2)
		log.Println("Bid trade succesfully fulfilled!")
	}
	err := redis_client.SaveTrade(user_trade)
	if err != nil {
		log.Fatalln("error during getting trading amount: ", err)
	}
	bot.BitsoOId = oid
}

func (bot *TradingBot) setOrder(ticker *bitso.Ticker, rate float64, order_type bitso.OrderType, threshold float64, market_trade string, total, amount, locked float64, book bitso.Book, book_fee bitso.Fee, side bitso.OrderSide, redis_client *database.RedisClient) {
	var order *bitso.OrderPlacement
	if side.String() == "sell" {
		order = &bitso.OrderPlacement{
			Book:  book,
			Side:  side,
			Type:  order_type,
			Major: bitso.Monetary("0.0"),
			Minor: bitso.ToMonetary(amount),
			Price: bitso.ToMonetary(rate),
		}
		oid, err := bot.BitsoClient.PlaceOrder(order)
		if err != nil {
			log.Fatalln("Error placing order: ", err)
		}
		bot.BitsoOId = oid
		err = updateBalance(total, 0.0, total, book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		err = updateBalance(0.0, locked, 0.0, book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.Side = bitso.OrderSide(1)
	} else {
		order = &bitso.OrderPlacement{
			Book:  book,
			Side:  side,
			Type:  order_type,
			Major: bitso.ToMonetary(amount),
			Minor: bitso.Monetary("0.0"),
			Price: bitso.ToMonetary(rate),
		}
		oid, err := bot.BitsoClient.PlaceOrder(order)
		if err != nil {
			log.Fatalln("Error placing order: ", err)
		}
		bot.BitsoOId = oid
		err = updateBalance(0.0, locked, 0.0, book.Minor(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		err = updateBalance(total, 0.0, total, book.Major(), redis_client)
		if err != nil {
			log.Fatalln("error during updating balance: ", err)
		}
		bot.Side = bitso.OrderSide(2)
	}
	log.Panicln("Ask trade successfully fulfill")
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
func stopSimulation() {
	log.Fatalln("Simulation stopped")
}
