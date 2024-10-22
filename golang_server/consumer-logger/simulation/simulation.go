package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/url"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/queue"
	"github.com/segmentio/kafka-go/example/consumer-logger/table"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
)

var wg sync.WaitGroup
var mutex sync.Mutex

const MaxActiveOrders = 1
const DEBUG = false
const STAGE = false

type TradingBot struct {
	BitsoClient                         *bitso.Client
	DBClient                            interface{}
	KafkaClient                         *queue.KafkaClient
	BitsoBook                           bitso.Book
	BitsoExchangeBook                   bitso.ExchangeOrderBook
	BitsoOId                            string
	BitsoLastCompleteOId                string
	BitsoActiveOIds                     []string
	MajorBalance                        bitso.Balance
	MinorBalance                        bitso.Balance
	MajorActiveOrders                   []string
	MinorActiveOrders                   []string
	Side                                bitso.OrderSide
	BidFirst                            bool
	Threshold                           float64
	OrderSet                            chan bool
	MajorProfit                         TradingProfit
	MinorProfit                         TradingProfit
	MaxPublicRequestPerMinute           int8
	MaxPrivateRequestPerMinute          int8
	NumBatches                          int
	Debug                               bool
	Stage                               bool
	SetNewOrder                         bool
	BidTradeBatch                       queue.BidTradeTrendConsumer
	LastTradeBatchId                    int
	NextTradeBatchId                    int
	UniqueBatchIDs                      []int
	CumPercentageChange                 float64
	CumPercentageChangeByLastNthBatches float64
	CountCancelationOrders              int8
	MinMinorAmountToTrade               float64
	MaxMinorAmountToTrade               float64
	MinMajorAmountToTrade               float64
	MaxMajorAmountToTrade               float64
	TradingFrequency                    int8
	TableData                           *table.TableData
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

func NewTradingBot(b_client *bitso.Client, db_client interface{}, debug, stage bool, book *bitso.Book) *TradingBot {
	return &TradingBot{
		BitsoClient:                b_client,
		DBClient:                   db_client,
		BitsoBook:                  *book,
		MaxPublicRequestPerMinute:  60,
		MaxPrivateRequestPerMinute: 100,
		Side:                       bitso.OrderSide(1),
		BidFirst:                   true,
		Threshold:                  0.1,
		Debug:                      debug,
		Stage:                      stage,
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

	bot := NewTradingBot(bitso_client, redis_client, DEBUG, STAGE, book)
	bot.setBitsoClient()
	bot.setInitialConfg()

	bot.startTrading()
	// go bot.generateBidTrade()
	// Run the simulation for a certain period
	endTime := time.Now().Add(12 * time.Hour) // Simulation for 12 hours
	for time.Now().Before(endTime) {
		log.Println("Simulation started at:", time.Now())
		log.Println("Simulation will end at:", endTime)
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
	bot.setTradingFrequency()
	bot.setMinMinorAmountToTrade()
	bot.setMaxMinorAmountToTrade()
	bot.setInitFees(redis_client)
	bot.setInitExchangeOrderBooks(redis_client)
	bot.setInitTableObject()
	bot.clearRedisWsBatchTradeIdsList(redis_client)
	bot.clearRedisUserOrders(redis_client)
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

func (bot *TradingBot) setInitTableObject() {
	bot.TableData = table.NewTableData()
}

func (bot *TradingBot) clearRedisWsBatchTradeIdsList(redis_client *database.RedisClient) {
	listName := "batchIdsUsed"
	keyPattern := "batchid_*"
	err := redis_client.ClearBatchIDsList(listName)
	if err != nil {
		log.Fatalln("error deleting unique batch ids list: ", err)
	}

	err = redis_client.DeleteKafkaBatchKeysByPattern(keyPattern)
	if err != nil {
		log.Fatalln("error deleting ws trade batches: ", err)
	}
	log.Println("successfully deleted all ws trade batch records from redis!")
}

func (bot *TradingBot) clearRedisUserOrders(redis_client *database.RedisClient) {
	err := redis_client.DeleteAllUserOrders()
	if err != nil {
		log.Fatalln("error deleting ws trade batches: ", err)
	}
	log.Println("successfully deleted all user orders saved in redis!")
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
			bot.setOrder(ticker, currency_amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
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

func (bot *TradingBot) setTradingFrequency() {
	bot.TradingFrequency = 5
}

func (bot *TradingBot) setMinMinorAmountToTrade() {
	bot.TradingFrequency = 10
}

func (bot *TradingBot) setMaxMinorAmountToTrade() {
	bot.TradingFrequency = 100
}

func (bot *TradingBot) startTrading() {
	var (
		testKafka bool
	)
	redis_client, err := database.SetupRedis()
	if err != nil {
		log.Fatalln("Failed to connect to Redis:", err)
	}
	defer redis_client.CloseDB()
	book := bot.BitsoBook
	if bot.Side.String() == "sell" && bot.MajorBalance.Available.Float64() <= 0.0 || bot.Side.String() == "buy" && bot.MinorBalance.Available.Float64() <= 0.0 {
		log.Fatalln("error user balance is 0: ", err)
	}
	book_fee, err := redis_client.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting book fee: ", err)
	}
	// ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
	// if err != nil {
	// 	log.Fatalln("error getting ticket request")
	// }
	// ask_rate := ticker.Ask.Float64()
	// bid_rate := ticker.Bid.Float64()
	done := make(chan bool)
	done2 := make(chan bool)

	kafka_avg_price_task_done := make(chan bool)
	kafka_ws_trade_trends_task_done := make(chan bool)
	// kafka_avg_price := make(chan queue.KafkaConsumer)
	kafka_ws_trade_trends := make(chan queue.BidTradeTrendConsumer)
	// kafka_price_trend := make(chan queue.KafkaConsumer)
	var k_wg sync.WaitGroup
	// go bot.KafkaAvgPricesConsumer(kafka_avg_price, &k_wg)
	// go bot.KafkaPriceTrendConsumer(kafka_price_trend, &k_wg)
	if testKafka {
		go bot.KafkaWsTradesConsumer(kafka_ws_trade_trends, &k_wg)
		k_wg.Wait()
		log.Println("end kafka now continue!")

	}
	// sleep := (10 * time.Second)
	// wg.Add(1)
	log.Println("Start")
	for {
		go bot.OrderMaker(book, book_fee, done, redis_client)
		go bot.RenewOrCancelOrder(book, book_fee, done2, redis_client)
		// go func() {
		// 	kafkaData := <-kafka_avg_price // Receive the value from the channel

		// 	// Create a zero-value instance of KafkaConsumer struct
		// 	zeroKafkaData := queue.KafkaConsumer{}

		// 	if kafkaData != zeroKafkaData {
		// 		// Access the fields and perform further operations
		// 		ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
		// 		if err != nil {
		// 			log.Fatalln("error getting ticket request")
		// 		}
		// 		ask_rate = ticker.Ask.Float64()
		// 		bid_rate = ticker.Bid.Float64()
		// 		w := newTabWriter()
		// 		fmt.Fprintf(w, "WINDOW_START\tWINDOW_END\tNEXT_MINUTE\tAVG_BID\tSTD_DEV_BID\tAVG_ASK\tSTD_DEV_ASK\n")
		// 		fmt.Fprintf(w, "%s\t%s\t%s\t%f\t%f\t%f\t%f\n",
		// 			kafkaData.WindowStart,
		// 			kafkaData.WindowEnd,
		// 			kafkaData.NextMinute,
		// 			kafkaData.AvgBid,
		// 			kafkaData.BidStdDev,
		// 			kafkaData.AvgAsk,
		// 			kafkaData.AskStdDev,
		// 		)
		// 		w.Flush()
		// 		w = newTabWriter()
		// 		fmt.Fprintf(w, "CURRENT_TIME\tBID_RATE\tASK_RATE\n")
		// 		fmt.Fprintf(w, "%s\t%f\t%f\n",
		// 			time.Now(),
		// 			bid_rate,
		// 			ask_rate,
		// 		)
		// 		w.Flush()
		// 		kafka_avg_price_task_done <- true
		// 	} else {
		// 		fmt.Println("kafkaData is empty or nil")
		// 	}
		// }()

		// go func() {
		// 	kafkaData := <-kafka_ws_trade_trends // Receive the value from the channel
		// 	num_batches := 2
		// 	// Create a zero-value instance of KafkaConsumer struct
		// 	zeroKafkaData := queue.BidTradeTrendConsumer{}

		// 	if kafkaData != zeroKafkaData {
		// 		mutex.Lock()
		// 		err := redis_client.SaveBatchTrade(kafkaData)
		// 		if err != nil {
		// 			log.Fatalln("error saving trades")
		// 		}

		// 		if !utils.ContainsInt(bot.UniqueBatchIDs, kafkaData.BatchId) {
		// 			// Append tradeBatchID to the slice if it's not present
		// 			bot.UniqueBatchIDs = append(bot.UniqueBatchIDs, kafkaData.BatchId)
		// 		}

		// 		// Access the fields and perform further operations
		// 		ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
		// 		if err != nil {
		// 			log.Fatalln("error getting ticket request")
		// 		}
		// 		ask_rate = ticker.Ask.Float64()
		// 		bid_rate = ticker.Bid.Float64()
		// 		time_now := time.Now()
		// 		w := newTabWriter()
		// 		fmt.Fprintf(w, "BATCH_ID\tSIDE\tWINDOW_START\tWINDOW_END\tMAX_PRICE\tMIN_PRICE\tVAR\tSTD_DEV\tAVG\tTREND\n")
		// 		fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%f\t%f\t%f\t%f\t%f\t%d\n",
		// 			kafkaData.BatchId,
		// 			kafkaData.Side,
		// 			kafkaData.WindowStart,
		// 			kafkaData.WindowEnd,
		// 			kafkaData.MaxPrice,
		// 			kafkaData.MinPrice,
		// 			kafkaData.Var,
		// 			kafkaData.StdDev,
		// 			kafkaData.MovingAverage,
		// 			kafkaData.Trend,
		// 		)
		// 		w.Flush()
		// 		fmt.Fprintf(w, "CURRENT_TIME\tBID_RATE\tASK_RATE\n")
		// 		fmt.Fprintf(w, "%s\t%f\t%f\n",
		// 			time_now,
		// 			bid_rate,
		// 			ask_rate,
		// 		)
		// 		bot.checkFunds()
		// 		w.Flush()
		// 		// log.Println("kafkaData.BatchId: ", kafkaData.BatchId, " bot.LastTradeBatchId: ", bot.LastTradeBatchId, " bot.NextTradeBatchId: ", bot.NextTradeBatchId)
		// 		bot.CalculateCumulativePercentageChange(kafkaData)
		// 		if (bot.LastTradeBatchId == bot.NextTradeBatchId) ||
		// 			(kafkaData.BatchId > bot.NextTradeBatchId) {
		// 			bot.LastTradeBatchId = kafkaData.BatchId
		// 			bot.NextTradeBatchId = kafkaData.BatchId + 2
		// 		}

		// 		bot.checkCumBatches(num_batches, kafkaData, redis_client)
		// 		bot.checkOrderPriceToAvgTradePrices(kafkaData, book_fee.FeeDecimal, redis_client)
		// 		mutex.Unlock()
		// 		kafka_ws_trade_trends_task_done <- true
		// 	}
		// }()

		select {
		case <-done:
			// Method completed successfully, resume loop
			fmt.Println("gorutine OrderMaker done")
		case <-time.After(300 * time.Minute):
			fmt.Println("Timed out")
			return
		case <-done2:
			fmt.Println("gorutine RenewOrCancelOrder done")
			sleepDuration := 1 * time.Minute
			time.Sleep(sleepDuration)
		case <-kafka_avg_price_task_done:
			log.Println("Kafka avg prices done! Now check current prices with kafka data ")
		case <-kafka_ws_trade_trends_task_done:
			log.Println("Kafka price trends done! Now check current prices with kafka data ")
		}
	}
}

func (bot *TradingBot) OrderMaker(book bitso.Book, book_fee bitso.Fee, done chan bool, redis_client *database.RedisClient) {
	time_ticker := time.NewTicker(30 * time.Second)
	var (
		rate         float64
		major_amount float64
	)
	const market_trade = "maker"
	defer time_ticker.Stop()
	for range time_ticker.C {
		log.Println("30 seconds have passed")
		bot.checkFunds()
		mutex.Lock()
		/* TOREDO when buy or sell this condition will
		prevent from setting a bid/ask order from previous trade
		*/
		ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
		if err != nil {
			log.Fatalln("error getting ticket request")
		}
		ask_rate := ticker.Ask.Float64()
		bid_rate := ticker.Bid.Float64()
		// This condition is refactored in a method inside bot package
		if bot.Stage {
			major_amount = bot.getMinMajorAmount()
		} else {
			major_amount = bot.getMinMinorValue() / bid_rate
		}
		minor_amount := bot.getMinMinorValue()
		order_type := bitso.OrderType(2)
		x := 0.5
		rand.Seed(time.Now().UnixNano())
		randomNumber := rand.Float64()
		oid := utils.GenRandomOId(7)
		rate = bot.getOptimalPrice(ticker, market_trade, bot.Side.String())
		if rate > 0 {
			if !bot.Debug {
				if bot.Side.String() == "buy" {
					currency_amount := minor_amount / bid_rate // BTC
					log.Println("amount as minor: ", currency_amount*rate)
					bot.setOrder(ticker, currency_amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
				} else {
					currency_amount := major_amount // * bid_rate // BTC
					log.Println("amount as minor: ", currency_amount*rate)
					bot.setOrder(ticker, currency_amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
				}
			} else {
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
			}
		}
		mutex.Unlock()
		done <- true
	}
}

func (bot *TradingBot) RenewOrCancelOrder(book bitso.Book, book_fee bitso.Fee, done chan bool, redis_client *database.RedisClient) {
	/*
		Checks orders with 'on pending' state and deletes
		them if 1 minute has passed.
	*/
	time_ticker := time.NewTicker(1*time.Minute + 20*time.Second)
	defer time_ticker.Stop()
	for range time_ticker.C {
		if len(bot.BitsoOId) > 0 {
			log.Println("1 minute and 10 seconds have passed")
			mutex.Lock()
			bot.checkFunds()
			ticker, err := bot.BitsoClient.Ticker(&book) // btc_mxn
			if err != nil {
				log.Fatalln("error getting ticket request")
			}
			num_of_current_active_orders := len(bot.BitsoActiveOIds)
			if bot.CountCancelationOrders > 10 {
				bot.CountCancelationOrders = 0
			}
			if !bot.checkActiveOrders() {
				current_oid := bot.BitsoOId
				bot.limitActiveOrders(num_of_current_active_orders, redis_client)
				if bot.checkOrderStatus("completed", redis_client) {
					log.Println("user order was completed!")
					user_trade, err := bot.getUserTradeByOId(bot.BitsoOId)
					if err != nil || user_trade.Major.Float64() == 0 {
						log.Fatalln("error while getting user trade: ", err)
					}
					arr := make([]string, 0)
					bot.updateCompleteOrders(bot.BitsoOId, user_trade.Side.String(), arr)
					if bot.Side.String() == "sell" {
						log.Println("bot side change to buy")
						bot.Side = bitso.OrderSide(1)
						if !bot.BidFirst {
							bot.BitsoLastCompleteOId = bot.BitsoOId
						} else {
							bot.BitsoLastCompleteOId = ""
						}
					} else {
						log.Println("bot side change to sell")
						bot.Side = bitso.OrderSide(2)
						if !bot.BidFirst {
							bot.BitsoLastCompleteOId = ""
						} else {
							bot.BitsoLastCompleteOId = bot.BitsoOId
						}
					}
					bot.CountCancelationOrders = 0

				} else {
					bot.renewOrder(ticker, redis_client)
					if current_oid == bot.BitsoOId {
						log.Println("current_oid == bot.BitsoOId: ", current_oid, bot.BitsoOId)
						bot.cancelOrder(bot.BitsoOId, redis_client)
						bot.CountCancelationOrders += 1
						log.Println("count cancelation is now: ", bot.CountCancelationOrders)
					}
				}
			} else {
				log.Println("currently, there are no active orders")
			}
			mutex.Unlock()
		}
		done <- true
	}
}

func (bot *TradingBot) setBitsoClient() {
	var (
		key, secret string
	)
	if bot.Stage {
		key = os.Getenv("STAGE_BITSO_API_KEY")
		secret = os.Getenv("STAGE_BITSO_API_SECRET")
	} else {
		key = os.Getenv("BITSO_API_KEY")
		secret = os.Getenv("BITSO_API_SECRET")
	}
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
		amount       float64
		value        float64
		total        float64
		taker_fee    float64
		maker_fee    float64
		last_trade   bitso.UserTrade
		major_amount float64
	)
	book := bot.BitsoBook
	lower_limit := bot.getTradingLowerLimit() // MXN
	upper_limit := bot.getTradingUpperLimit() // MXN
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
	if len(bot.BitsoLastCompleteOId) > 0 {
		bot.BitsoOId = bot.BitsoLastCompleteOId
	}
	if len(bot.BitsoOId) == 0 {
		log.Println("Condition met: 'len(bot.BitsoOId) == 0' ")
		if bot.Stage {
			major_amount = bot.getMinMajorAmount()
		} else {
			major_amount = bot.getMinMinorValue() / bid_rate
		}
		minor_amount := bot.getMinMinorValue()
		if market_side == "sell" {
			amount = major_amount
			value = amount * bid_rate
			if market_trade == "taker" {
				taker_fee = value * fee.TakerFeeDecimal.Float64()
				total = (value + taker_fee) / amount // MXN
				// stop loss
				if bid_rate < (value - taker_fee - lower_limit) {
					total = value - lower_limit
				}
			} else {
				maker_fee = (value * fee.MakerFeeDecimal.Float64())
				total = (value + maker_fee) / amount // MXN
				// stop loss
				if bid_rate < (value - maker_fee - lower_limit) {
					total = value - lower_limit
				}
			}
			ask_value := total * amount
			price_percentage_change := ((bid_rate / total) - 1) * 100
			log.Println("fee.FeeDecimal.Float64(): ", fee.FeeDecimal.Float64(), ", taker_fee: ", taker_fee, ", maker_fee: ", maker_fee)
			bot.TableData.GetTableOptimalPriceSummary("sell", amount, bid_rate, ask_value, taker_fee, maker_fee, total, price_percentage_change)
			if price_percentage_change > 0 {
				log.Printf("Price change is: %f%%", price_percentage_change)
				return bid_rate
			}
			total -= (bot.getWeightedBidAskSpread())
			return utils.RoundToNearestTen(total)
		} else {
			amount = minor_amount // MXN
			value = amount / ask_rate
			// upper_limit = upper_limit / ask_rate
			if market_trade == "taker" {
				taker_fee = value * fee.FeeDecimal.Float64()
				total = amount / (value + taker_fee + upper_limit) // BTC
			} else {
				maker_fee = (value * fee.FeeDecimal.Float64()) * 0.1
				total = amount / (value + maker_fee) // BTC
			}
			bid_value := amount / total
			price_percentage_change := ((ask_rate / total) - 1) * 100
			bot.TableData.GetTableOptimalPriceSummary("buy", amount, ask_rate, bid_value, taker_fee, maker_fee, total, price_percentage_change)

			if price_percentage_change < 0 {
				log.Printf("Price change is %f%", price_percentage_change)
				return ask_rate
			}
			total += (bot.getWeightedBidAskSpread() / ask_rate)
			rounded_total := utils.RoundToNearestTen(total)
			final_total := bot.checkTotalAndRoundedTotal(total, rounded_total)
			log.Println("final_total: ", final_total, ", rounded_total: ", rounded_total, ", total: ", total)
			return final_total
		}
	} else if (len(bot.BitsoOId)) > 0 && (len(bot.BitsoActiveOIds)) == 0 && !bot.checkOrderStatus("open", redis_client) {
		log.Println("len(bot.BitsoOId) > 0 and order is completed")
		if !bot.Debug {
			last_user_order_trade, err := bot.getLastUserTradeByOId(bot.BitsoLastCompleteOId)
			if err != nil || last_user_order_trade.Major.Float64() == 0 {
				log.Fatalln("error while getting user last trade: ", err)
			}
			if market_side == "sell" {
				amount = last_user_order_trade.Major.Float64() - last_user_order_trade.FeesAmount.Float64() // BTC
				value = amount * bid_rate                                                                   // MXN
				change := -last_user_order_trade.Minor.Float64() - value                                    // MXN
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
				bot.TableData.GetTableOptimalPriceSummary("sell", amount, bid_rate, ask_value, taker_fee, maker_fee, total, price_percentage_change)
				if price_percentage_change > 0 {
					log.Printf("Price change is: %f%%", price_percentage_change)
					return bid_rate
				}
				if !bot.BidFirst {
					total -= bot.getWeightedBidAskSpread() // MXN
				}
				return utils.RoundToNearestTen(total)
			} else {
				amount = last_trade.Minor.Float64() - last_trade.FeesAmount.Float64() // MXN
				value = amount / ask_rate
				change := -last_trade.Major.Float64() - value // Assuming it was an ask trade
				// upper_limit = upper_limit / ask_rate
				if market_trade == "taker" {
					taker_fee = value * fee.FeeDecimal.Float64()
					total = amount / (value + change + taker_fee) // BTC
				} else {
					maker_fee = value * fee.FeeDecimal.Float64()
					total = amount / (value + change + maker_fee) // BTC
				}
				bid_value := amount / total
				price_percentage_change := ((ask_rate / total) - 1) * 100
				bot.TableData.GetTableOptimalPriceSummary("buy", amount, ask_rate, bid_value, taker_fee, maker_fee, total, price_percentage_change)
				if price_percentage_change < 0 {
					log.Printf("Price change is %f%%", price_percentage_change)
					return ask_rate
				}
				if bot.BidFirst {
					total += (bot.getWeightedBidAskSpread() / ask_rate) // BTC
				}
				return utils.RoundToNearestTen(total)
			}
		} else {
			last_trade, err = redis_client.GetUserTrade(bot.BitsoOId)
			if err != nil {
				log.Fatalln("error getting user last trade: ", err)
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
				bot.TableData.GetTableOptimalPriceSummary("sell", amount, bid_rate, ask_value, taker_fee, maker_fee, total, price_percentage_change)
				if price_percentage_change > 0 {
					log.Printf("Price change is: %f%%", price_percentage_change)
					return bid_rate
				}
				return utils.RoundToNearestTen(total)
			} else {
				amount = last_trade.Minor.Float64() - last_trade.FeesAmount.Float64() // MXN
				value = amount / ask_rate
				change := -last_trade.Major.Float64() - value // Assuming it was an ask trade
				// upper_limit = upper_limit / ask_rate
				if market_trade == "taker" {
					taker_fee = value * fee.FeeDecimal.Float64()
					total = amount / (value + change + taker_fee) // BTC
				} else {
					maker_fee = value * fee.FeeDecimal.Float64()
					total = amount / (value + change + maker_fee) // BTC
				}
				bid_value := amount / total
				price_percentage_change := ((ask_rate / total) - 1) * 100
				bot.TableData.GetTableOptimalPriceSummary("buy", amount, ask_rate, bid_value, taker_fee, maker_fee, total, price_percentage_change)
				if price_percentage_change < 0 {
					log.Printf("Price change is %f%%", price_percentage_change)
					return ask_rate
				}
				return utils.RoundToNearestTen(total)
			}
		}
	}
	return total
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

// Called only during trading simulation
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
		value = amount * rate // Minor
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
			Major:        bitso.ToMonetaryWithP(value),
			CreatedAt:    created_at,
			Minor:        bitso.ToMonetary(-amount),
			FeesAmount:   bitso.ToMonetaryWithP(fee),
			FeesCurrency: book.Major(),
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

func (bot *TradingBot) setOrder(ticker *bitso.Ticker, amount, rate float64, oid string, order_status bitso.OrderStatus, order_type bitso.OrderType, redis_client *database.RedisClient) {
	var order *bitso.OrderPlacement
	if !bot.Debug {
		if bot.Side.String() == "sell" {
			order = &bitso.OrderPlacement{
				Book:  bot.BitsoBook,
				Side:  bot.Side,
				Type:  order_type,
				Major: bitso.ToMonetary(amount),
				Minor: "",
				Price: bitso.ToMonetary(rate),
			}
			log.Println("final order details")
			bot.TableData.GetTableOrderPlacement(bot.BitsoBook, bot.Side, order_type, bitso.ToMonetary(amount), "", bitso.ToMonetary(rate))
			log.Println("amount as minor (value): ", amount*rate)
			oid, err := bot.BitsoClient.PlaceOrder(order)
			if err != nil {
				errorCode, amount, _ := utils.ParseErrorMessage(err)
				if errorCode == 405 {
					log.Println(err)
					bot.setOrder(ticker, amount, rate, oid, order_status, order_type, redis_client)
					return
				}
				log.Fatalln("Error placing ask order: ", err)
			}
			bot.BitsoOId = oid
			bot.BitsoActiveOIds = append(bot.BitsoActiveOIds, oid)
			bot.MajorActiveOrders = append(bot.MajorActiveOrders, oid)
		} else {
			order = &bitso.OrderPlacement{
				Book:  bot.BitsoBook,
				Side:  bot.Side,
				Type:  order_type,
				Major: bitso.ToMonetary(amount),
				Minor: "",
				Price: bitso.ToMonetary(rate),
			}
			log.Println("final order details")
			bot.TableData.GetTableOrderPlacement(bot.BitsoBook, bot.Side, order_type, bitso.ToMonetary(amount), "", bitso.ToMonetary(rate))
			log.Println("amount as major (value): ", amount*rate)
			oid, err := bot.BitsoClient.PlaceOrder(order)
			if err != nil {
				errorCode, minAmount, _ := utils.ParseErrorMessage(err)
				if errorCode == 403 {
					log.Println(err)
					bot.setOrder(ticker, minAmount, rate, oid, order_status, order_type, redis_client)
					return
				} else if errorCode == 405 {
					log.Println(err)
					ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook) // btc_mxn
					if err != nil {
						log.Fatalln("error getting ticket request")
					}
					bid_rate := ticker.Bid.Float64()
					toAdd := ((amount * rate) - (minAmount)) / bid_rate
					bot.setOrder(ticker, amount+toAdd, bid_rate, oid, order_status, order_type, redis_client)
					return
				}
				log.Fatalln("error placing bid order: ", err)
			}
			bot.BitsoOId = oid
			bot.BitsoActiveOIds = append(bot.BitsoActiveOIds, oid)
			bot.MinorActiveOrders = append(bot.MinorActiveOrders, oid)
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
		// oid := utils.GenRandomOId(7)
		// order_status := bitso.OrderStatus(1)
		if bot.Side.String() == "sell" {
			user_order := &bitso.UserOrder{
				Book:           bot.BitsoBook,
				OriginalAmount: bitso.ToMonetary(amount),        // Major
				UnfilledAmount: bitso.ToMonetary(amount),        // Major
				OriginalValue:  bitso.ToMonetary(amount * rate), // Minor
				CreatedAt:      bitso.Time(time.Now()),
				UpdatedAt:      bitso.Time(time.Now()),
				Price:          bitso.ToMonetary(rate), // Minor
				OID:            oid,
				Side:           bot.Side,
				Status:         order_status,
				Type:           order_type.String(),
			}
			log.Println("final order (ask) details: ", user_order)
			log.Println("amount as minor (value): ", amount*rate)
			log.Println("amount (BTC): ", amount, ", rate (MXN): ", rate, ", ")
			redis_client.SaveUserOrder(*user_order)
			// bot.BitsoOId = oid
			if order_status.String() == "open" {
				bot.BitsoActiveOIds = append(bot.BitsoActiveOIds, oid)
				bot.MajorActiveOrders = append(bot.MajorActiveOrders, oid)
			} else if order_status.String() == "completed" {
				temp := make([]string, 0)
				for _, active_oid := range bot.BitsoActiveOIds {
					if active_oid != oid {
						temp = append(temp, active_oid)
					}
					bot.BitsoActiveOIds = temp
				}
				temp = make([]string, 0)
				for _, active_oid := range bot.MajorActiveOrders {
					if active_oid != oid {
						temp = append(temp, active_oid)
					}
					bot.MajorActiveOrders = temp
				}
			}
			// bot.Side = bitso.OrderSide(1)
		} else {
			user_order := &bitso.UserOrder{
				Book:           bot.BitsoBook,
				OriginalAmount: bitso.ToMonetary(amount / rate), // Major
				UnfilledAmount: bitso.ToMonetary(amount / rate), // Major
				OriginalValue:  bitso.ToMonetary(amount),        // Minor
				CreatedAt:      bitso.Time(time.Now()),
				UpdatedAt:      bitso.Time(time.Now()),
				Price:          bitso.ToMonetary(rate),
				OID:            oid,
				Side:           bot.Side,
				Status:         order_status,
				Type:           order_type.String(),
			}
			log.Println("final order (bid) details: ", user_order)
			log.Println("amount as major (value): ", amount/rate)
			log.Println("amount (MXN): ", amount, ", rate (BTC): ", rate, ", order_status.String(): ", order_status.String())
			redis_client.SaveUserOrder(*user_order)
			// bot.BitsoOId = oid
			if order_status.String() == "open" {
				bot.BitsoActiveOIds = append(bot.BitsoActiveOIds, oid)
				bot.MinorActiveOrders = append(bot.MinorActiveOrders, oid)
			} else if order_status.String() == "completed" {
				temp := make([]string, 0)
				for _, active_oid := range bot.BitsoActiveOIds {
					if active_oid != oid {
						temp = append(temp, active_oid)
					}
					bot.BitsoActiveOIds = temp
				}
				temp = make([]string, 0)
				for _, active_oid := range bot.MinorActiveOrders {
					if active_oid != oid {
						temp = append(temp, active_oid)
					}
					bot.MinorActiveOrders = temp
				}
			}
			// bot.Side = bitso.OrderSide(2)
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

func (bot *TradingBot) checkConstrainedFunds() {
	if bot.MinorBalance.Total.Float64()-(bot.MinorBalance.Total.Float64()-100) < 0 {
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
	arr := make([]string, 0)
	if !bot.Debug {
		open_orders, err := bot.getUserOpenOrders()
		if err != nil {
			log.Fatalln("error during open orders request: ", err)
		}
		for _, open_order := range open_orders {
			if open_order.OID == oid {
				if open_order.Status == bitso.OrderStatus(1) {
					res, err := bot.BitsoClient.CancelOrder(oid)
					if err != nil {
						log.Fatalln("error during cancel order request: ", err)
					}
					bot.updateActiveOrders(oid, open_order.Side.String(), arr)
					log.Println("order cancelation request successfully fulfill: ", res)
				}
			}
		}
		if oid == bot.BitsoOId {
			bot.BitsoOId = ""
		}
	} else {
		user_order, err := redis_client.GetUserOrderById(oid)
		if err != nil {
			log.Fatalln("error getting user_order: ", err)
		}
		if len(user_order.OID) == 0 {
			log.Fatalln("user_order is empty: ", err, ", user_order.OID: ", user_order.OID, ", oid: ", oid)
		}
		user_order.Status = bitso.OrderStatus(4)
		err = redis_client.SaveUserOrder(user_order)
		if err != nil {
			log.Fatalln("error getting user_order!")
		}
		bot.updateActiveOrders(oid, user_order.Side.String(), arr)
		log.Println("order cancelation successfully fulfill")
	}
}

func (bot *TradingBot) cancelOrders(oids []string, redis_client *database.RedisClient) {

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
	// minor_amount := 12.0
	minor_amount := bot.MinorBalance.Available.Float64()
	if minor_amount > eo_book.MinimumValue.Float64() {
		return eo_book.MinimumValue.Float64()
	}
	return eo_book.MinimumValue.Float64()
}

func (bot *TradingBot) getMinMajorAmount() float64 {
	eo_book := bot.BitsoExchangeBook
	// major_amount := 0.000075
	major_amount := bot.MajorBalance.Available.Float64()
	if major_amount > eo_book.MinimumAmount.Float64() {
		return eo_book.MinimumAmount.Float64()
	}
	return eo_book.MinimumAmount.Float64()
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
	for _, oid := range bot.BitsoActiveOIds {
		fmt.Println("active oid: ", oid, ", bot.BitsoOId: ", bot.BitsoOId, ", len(bot.BitsoActiveOIds): ", len(bot.BitsoActiveOIds))
	}
	return len(bot.BitsoActiveOIds) == 0
}

func (bot *TradingBot) checkOrderStatus(order_status string, redis_client *database.RedisClient) bool {
	if len(bot.BitsoOId) == 0 {
		return false
	}
	if !bot.Debug {
		if order_status == "completed" {
			return bot.lookupTradeByOId(bot.BitsoOId)
		} else if order_status == "open" {
			// TODO instead of bot.BitsoOId should be bot.BitsoTId
			user_order, err := bot.BitsoClient.LookupOrder(bot.BitsoOId)
			if err != nil {
				log.Println("checkOrderStatus => error lookin up user order: ", err)
				return false
			}
			log.Println("looking up user_order: ", user_order)
			if user_order.Status.String() == order_status {
				return true
			}
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

func (bot *TradingBot) checkOrderPrice(oid string, ticker *bitso.Ticker, redis_client *database.RedisClient) {
	if len(bot.BitsoOId) > 0 {
		new_oid := utils.GenRandomOId(7)
		order_type := bitso.OrderType(2) // limit
		bid_rate := ticker.Bid.Float64()
		ask_rate := ticker.Ask.Float64()
		if !bot.Debug {
			user_order, err := bot.BitsoClient.LookupOrder(bot.BitsoOId)
			if err != nil {
				log.Fatalln("checkOrderPrice => error lookin up user order: ", err, " -> bot.BitsoOId: ", bot.BitsoOId)
			}
			if user_order.Side.String() == "sell" {
				rate := bid_rate
				amount := user_order.OriginalAmount.Float64() // Major
				if rate > user_order.Price.Float64() {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, rate, new_oid, bitso.OrderStatus(1), order_type, redis_client)
				}
			} else {
				rate := user_order.Price.Float64() / ask_rate
				amount := user_order.OriginalValue.Float64() // Minor
				if ask_rate < rate {
					bot.cancelOrder(oid, redis_client)
					rate = ticker.Bid.Float64()
					bot.setOrder(ticker, amount, rate, new_oid, bitso.OrderStatus(1), order_type, redis_client)
				}
			}
		} else {
			user_order, err := redis_client.GetUserOrderById(bot.BitsoOId)
			if err != nil {
				log.Fatalln("error getting user order", err)
			}
			if user_order.Side.String() == "sell" {
				rate := bid_rate
				amount := user_order.OriginalAmount.Float64()
				if ticker.Bid.Float64() > user_order.Price.Float64() {
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, rate, new_oid, bitso.OrderStatus(1), order_type, redis_client)
				}
			} else {
				rate := user_order.Price.Float64() / ask_rate
				amount := user_order.OriginalValue.Float64()
				if ask_rate < rate {
					rate = ticker.Bid.Float64()
					bot.cancelOrder(oid, redis_client)
					bot.setOrder(ticker, amount, rate, new_oid, bitso.OrderStatus(1), order_type, redis_client)
				}
			}
		}
	}
}

func (bot *TradingBot) KafkaPriceTrendConsumer(kafka_ch chan queue.KafkaConsumer, k_wg *sync.WaitGroup) {
	var reader queue.KafkaConsumer
	defer k_wg.Done()
	fmt.Println("KafkaPriceTrendConsumer start consuming ... !!")
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
	fmt.Println("KafkaAvgPricesConsumer start consuming ... !!")
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
			// fmt.Printf("message at topic: %v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
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

func (bot *TradingBot) KafkaWsTradesConsumer(kafka_ch chan queue.BidTradeTrendConsumer, k_wg *sync.WaitGroup) {
	var reader queue.BidTradeTrendConsumer
	// defer k_wg.Done()
	time_ticker := time.NewTicker(5 * time.Second)
	defer time_ticker.Stop()
	fmt.Println("KafkaWsTradesConsumer start consuming ... !!")
	for {
		k_wg.Add(1)
		select {
		case <-time_ticker.C:
			// Perform your periodic actions here
			log.Println("5 minutes has passed in Kafka Msg Service")
			m, err := bot.KafkaClient.Reader.ReadMessage(context.Background())
			if err != nil {
				log.Fatalln("error reading message for kafka: ", err)
			}
			// fmt.Printf("message at topic: %v partition:%v offset:%v	%s = %s\n", m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
			tmp_parser := struct {
				BatchId       int          `json:"batch_id"`
				Index         int          `json:"index"`
				Side          int          `json:"side"`
				Window        queue.Window `json:"window"`
				WindowStart   string       `json:"window_start"`
				WindowEnd     string       `json:"window_end"`
				MaxPrice      float64      `json:"max_price"`
				MinPrice      float64      `json:"min_price"`
				StdDev        float64      `json:"stddev"`
				MovingAverage float64      `json:"moving_avg"`
				Trend         int8         `json:"trend"`
			}{}
			err = json.Unmarshal(m.Value, &tmp_parser)
			if err != nil {
				log.Println("error while unmarshaled message: ", err)
			}
			const timeFormat = "2006-01-02T15:04:05.000-07"
			windowStart, err := time.Parse(timeFormat, tmp_parser.WindowStart)
			if err != nil {
				log.Fatalf("error parsing WindowStart: %v", err)
			}

			windowEnd, err := time.Parse(timeFormat, tmp_parser.WindowEnd)
			if err != nil {
				log.Fatalf("error parsing WindowEnd: %v", err)
			}

			// Subtract 1 hour from the time values
			windowStart = windowStart.Add(-time.Hour)
			windowEnd = windowEnd.Add(-time.Hour)

			reader.BatchId = tmp_parser.BatchId
			reader.Index = tmp_parser.Index
			reader.Side = tmp_parser.Side
			reader.Window = tmp_parser.Window
			reader.WindowStart = windowStart
			reader.WindowEnd = windowEnd
			reader.MaxPrice = tmp_parser.MaxPrice
			reader.MinPrice = tmp_parser.MinPrice
			reader.Var = (tmp_parser.StdDev * tmp_parser.StdDev)
			reader.StdDev = tmp_parser.StdDev
			reader.MovingAverage = tmp_parser.MovingAverage
			reader.Trend = tmp_parser.Trend
			// k_wg.Done()
			kafka_ch <- reader
		}
	}
}

func (bot *TradingBot) CalculateCumulativePercentageChange(batch queue.BidTradeTrendConsumer) {
	if (bot.BidTradeBatch == queue.BidTradeTrendConsumer{}) {
		log.Println("condition met bot.BidTradeBatch == queue.BidTradeTrendConsumer{} at CalculateCumulativePercentageChange ")
		bot.BidTradeBatch = batch
	}
	previousBatch := bot.BidTradeBatch
	currentBatch := batch
	// Calculate the percentage change in MovingAverage
	percentageChange := ((currentBatch.MovingAverage - previousBatch.MovingAverage) / previousBatch.MovingAverage) * 100

	bot.CumPercentageChange += math.Round(percentageChange*100) / 100
	bot.BidTradeBatch = batch
}

func (bot *TradingBot) checkOrderPriceToAvgTradePrices(kafkaData queue.BidTradeTrendConsumer, book_fee bitso.Monetary, redis_client *database.RedisClient) {
	var (
		amount       float64
		rate         float64
		market_trade string
		order_type   bitso.OrderType
		book         bitso.Book
		oid          string
	)
	market_trade = "maker"
	order_type = bitso.OrderType(2)
	ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook) // btc_mxn
	if err != nil {
		log.Fatalln("error getting bitso ticker API: ", err)
	}
	ask_rate := ticker.Ask.Float64()
	bid_rate := ticker.Bid.Float64()
	oid = utils.GenRandomOId(7)
	log.Println("BitsoActiveOIds: ", len(bot.BitsoActiveOIds), ", MinorActiveOrders: ", len(bot.MinorActiveOrders), ", MajorActivoeOrders: ", len(bot.MajorActiveOrders))
	if len(bot.BitsoActiveOIds) == 1 {
		for _, active_oid := range bot.BitsoActiveOIds {
			user_order, err := redis_client.GetUserOrderById(active_oid)
			if err != nil {
				log.Fatalln("err while fetching user order: ", err)
			}
			log.Println("user_order: ", user_order)
			if user_order.Status.String() == "open" {
				book = user_order.Book
				amount = user_order.OriginalValue.Float64() // Minor
				rate = bot.getOptimalPrice(ticker, market_trade, user_order.Side.String())
				x := 0.5
				rand.Seed(time.Now().UnixNano())
				randomNumber := rand.Float64()
				if bot.CumPercentageChange > 0 {
					log.Println("bot.CumPercentageChange > 0: ", bot.CumPercentageChange > 0)
					if !bot.Debug {
						if user_order.Side.String() == "sell" && kafkaData.Side == 1 {
							amount = user_order.OriginalAmount.Float64() // major
							if rate > user_order.Price.Float64() {
								if rate < kafkaData.MovingAverage {
									rate = kafkaData.MovingAverage
								}
								bot.cancelOrder(active_oid, redis_client)
								bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
							}
						} else if user_order.Side.String() == "buy" && kafkaData.Side == 2 {
							if bot.CumPercentageChangeByLastNthBatches > 0 {
								if rate > kafkaData.MovingAverage {
									rate = kafkaData.MovingAverage
								}
								bot.NumBatches += 1
							}
							bot.cancelOrder(active_oid, redis_client)
							bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
						}
					} else {
						if user_order.Side.String() == "sell" && kafkaData.Side == 1 {
							amount = user_order.OriginalAmount.Float64() // major
							if rate > user_order.Price.Float64() {
								log.Println("rate > user_order.Price")
								if rate < kafkaData.MovingAverage {
									rate = kafkaData.MovingAverage
								}
							}
							if randomNumber >= x {
								bot.setOrder(ticker, amount, rate, active_oid, bitso.OrderStatus(5), order_type, redis_client)
								bot.setUserTrade(rate, amount, book_fee, book, active_oid, redis_client)
							} else {
								bot.cancelOrder(active_oid, redis_client)
								bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
							}
						} else if user_order.Side.String() == "buy" && kafkaData.Side == 2 {
							if bot.CumPercentageChangeByLastNthBatches > 0 {
								if rate > kafkaData.MovingAverage {
									log.Println("rate > kafkaData.MovingAverage")
									rate = kafkaData.MovingAverage
								}
								bot.NumBatches += 1
							}
							if randomNumber >= x {
								bot.setOrder(ticker, amount, rate, active_oid, bitso.OrderStatus(5), order_type, redis_client)
								bot.setUserTrade(rate, amount, book_fee, book, active_oid, redis_client)
							} else {
								bot.cancelOrder(active_oid, redis_client)
								bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
							}
						}
					}
				} else {
					// what happends when cum percentage change is negative and I what to set a bid order?
					log.Println("bot.CumPercentageChange < 0: ", user_order.Side.String())
					if !bot.Debug {
						if user_order.Side.String() == "sell" && kafkaData.Side == 1 {
							amount = user_order.OriginalAmount.Float64() // major
							if bot.CumPercentageChangeByLastNthBatches < 0 {
								rate = bid_rate
								order_type = bitso.OrderType(1)
								bot.NumBatches += 1
							}
							bot.cancelOrder(active_oid, redis_client)
							bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
						} else if user_order.Side.String() == "buy" && kafkaData.Side == 2 {
							if rate < user_order.Price.Float64() {
								if rate > kafkaData.MovingAverage {
									rate = kafkaData.MovingAverage
								}
								order_type = bitso.OrderType(2)
							}
							bot.cancelOrder(active_oid, redis_client)
							bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
						}
					} else {
						if user_order.Side.String() == "sell" && kafkaData.Side == 1 {
							amount = user_order.OriginalAmount.Float64() // major
							if bot.CumPercentageChangeByLastNthBatches < 0 {
								if rate < kafkaData.MovingAverage {
									log.Println("rate < kafkaData.MovingAverage")
									rate = kafkaData.MovingAverage
								}
								bot.NumBatches += 1
							}
							if randomNumber >= x {
								bot.setOrder(ticker, amount, rate, active_oid, bitso.OrderStatus(5), order_type, redis_client)
								bot.setUserTrade(rate, amount, book_fee, book, active_oid, redis_client)
							} else {
								bot.cancelOrder(active_oid, redis_client)
								bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
							}
						} else if user_order.Side.String() == "buy" && kafkaData.Side == 2 {
							if rate < user_order.Price.Float64() {
								log.Println("rate < user_order.Price.Float64()")
								if rate > kafkaData.MovingAverage {
									rate = kafkaData.MovingAverage
								}
							}
							if randomNumber >= x {
								bot.setOrder(ticker, amount, rate, active_oid, bitso.OrderStatus(5), order_type, redis_client)
								bot.setUserTrade(rate, amount, book_fee, book, active_oid, redis_client)
							} else {
								bot.cancelOrder(active_oid, redis_client)
								bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
							}
						}
					}
				}
			}
		}
	} else {
		if len(bot.BitsoActiveOIds) < 1 {
			log.Println("setting initial order on Kafka Ws trades trend")
			// rate = bot.getOptimalPrice(ticker, market_trade, bot.Side.String())
			if bot.Side.String() == "sell" {
				rate = bid_rate
				amount = bot.getMinMinorValue() / bid_rate
			} else {
				rate = ask_rate
				amount = bot.getMinMinorValue()
			}
			bot.setOrder(ticker, amount, rate, oid, bitso.OrderStatus(1), order_type, redis_client)
		}
	}
}

func (bot *TradingBot) checkCumBatches(num_batches int, kafkaData queue.BidTradeTrendConsumer, redis_client *database.RedisClient) {
	least_elem_arr := num_batches + 1
	if len(bot.UniqueBatchIDs) > 1 && (len(bot.UniqueBatchIDs)%least_elem_arr) == 0 {
		log.Println("Condition meet to calculate cum PCh by last N batches")
		bid_trade_batches, err := redis_client.GetLastNthBatches(2)
		if err != nil {
			log.Fatalln("err while fetching batch bid trades: ", err)
		}
		bot.CumPercentageChangeByLastNthBatches = queue.CalculateCumulativePercentageChange(bid_trade_batches)
		log.Println("cum_trend: ", bot.CumPercentageChange, " cum_trend_taken_last_n_batches: ", bot.CumPercentageChangeByLastNthBatches)

		bot.NextTradeBatchId = kafkaData.BatchId + 2
		bot.LastTradeBatchId = kafkaData.BatchId
		bot.UniqueBatchIDs = make([]int, 0)
	}
}

func (bot *TradingBot) getUserOpenOrders() ([]bitso.UserOrder, error) {
	params := url.Values{}
	params.Add("book", bot.BitsoBook.String())
	user_orders, err := bot.BitsoClient.MyOpenOrders(params)
	if err != nil {
		return nil, err
	}
	return user_orders, nil
}

func (bot *TradingBot) checkMaxOpenOrders(user_orders []bitso.UserOrder) bool {
	return len(user_orders) > MaxActiveOrders
}

func (bot *TradingBot) cancelOverflownOpenOrders(user_orders []bitso.UserOrder, redis_client *database.RedisClient) {
	if bot.checkMaxOpenOrders(user_orders) {
		current_active_orders := len(user_orders)
		for _, user_order := range user_orders {
			if current_active_orders == MaxActiveOrders {
				break
			} else {
				if user_order.OID != bot.BitsoOId {
					_, err := bot.BitsoClient.CancelOrder(user_order.OID)
					if err != nil {
						log.Fatalln("error during cancel order request: ", err)
					}
					current_active_orders -= 1
				}
			}
		}
	}
}

func (bot *TradingBot) updateActiveOrders(oid, side string, arr []string) {
	for _, active_order := range bot.BitsoActiveOIds {
		if active_order != oid {
			arr = append(arr, active_order)
		}
	}
	bot.BitsoActiveOIds = arr
	if side == "sell" {
		arr = make([]string, 0)
		for _, ask_active_order := range bot.MajorActiveOrders {
			if ask_active_order != oid {
				arr = append(arr, ask_active_order)
			}
		}
		bot.MajorActiveOrders = arr
	} else {
		arr = make([]string, 0)
		for _, bid_active_order := range bot.MinorActiveOrders {
			if bid_active_order != oid {
				arr = append(arr, bid_active_order)
			}
		}
		bot.MinorActiveOrders = arr
	}
}

func (bot *TradingBot) renewOrder(ticker *bitso.Ticker, redis_client *database.RedisClient) {
	for _, oid := range bot.BitsoActiveOIds {
		if bot.Side.String() == "sell" {
			bot.checkOrderPrice(oid, ticker, redis_client)
		} else {
			bot.checkOrderPrice(oid, ticker, redis_client)
		}
	}
}

func (bot *TradingBot) limitActiveOrders(num_of_current_active_orders int, redis_client *database.RedisClient) {
	if num_of_current_active_orders > MaxActiveOrders {
		for _, oid := range bot.BitsoActiveOIds {
			if len(bot.BitsoActiveOIds) == MaxActiveOrders {
				break
			} else {
				bot.cancelOrder(oid, redis_client)
			}
		}
	}
}

func (bot *TradingBot) updateCompleteOrders(oid, side string, arr []string) {
	for _, active_order := range bot.BitsoActiveOIds {
		if active_order != oid {
			arr = append(arr, active_order)
		}
	}
	bot.BitsoActiveOIds = arr
	if side == "sell" {
		arr = make([]string, 0)
		for _, ask_active_order := range bot.MajorActiveOrders {
			if ask_active_order != oid {
				arr = append(arr, ask_active_order)
			}
		}
		bot.MajorActiveOrders = arr
	} else {
		arr = make([]string, 0)
		for _, bid_active_order := range bot.MinorActiveOrders {
			if bid_active_order != oid {
				arr = append(arr, bid_active_order)
			}
		}
		bot.MinorActiveOrders = arr
	}
}

// func (bot *TradingBot) updateCompleteOrders(num_of_current_active_orders int, redis_client *database.RedisClient) {
// 	if num_of_current_active_orders > MaxActiveOrders {
// 		for _, oid := range bot.BitsoActiveOIds {
// 			if len(bot.BitsoActiveOIds) == MaxActiveOrders {
// 				break
// 			} else {
// 				bot.cancelOrder(oid, redis_client)
// 			}
// 		}
// 	}
// }

func (bot *TradingBot) lookupTradeByOId(oid string) bool {
	user_trades, err := bot.BitsoClient.OrderTrades(oid, nil)
	if err != nil {
		log.Println("error getting requested user trade: ", err)
	}
	for _, user_trade := range user_trades {
		log.Println("lookupTradeByOId -> user_trade: ", user_trade)
		// error
		if user_trade.Side.String() == bot.Side.String() {
			return true
		}
	}
	return false
}

func (bot *TradingBot) getUserTradeByOId(oid string) (bitso.UserOrderTrade, error) {
	empty_ut := bitso.UserOrderTrade{}
	user_trades, err := bot.BitsoClient.OrderTrades(oid, nil)
	if err != nil {
		return empty_ut, err
	}
	log.Println("len: ", len(user_trades))
	for _, user_trade := range user_trades {
		log.Println("getUserTradeByOId -> user_trade: ", user_trade)
		if bot.Side.String() == user_trade.Side.String() {
			return user_trade, nil

		}
	}
	return empty_ut, nil
}

func (bot *TradingBot) getLastUserTradeByOId(oid string) (bitso.UserOrderTrade, error) {
	empty_ut := bitso.UserOrderTrade{}
	user_trades, err := bot.BitsoClient.OrderTrades(oid, nil)
	if err != nil {
		return empty_ut, err
	}
	log.Println("len: ", len(user_trades))
	for _, user_trade := range user_trades {
		log.Println("last_user_trade: ", user_trade)
		if bot.Side.String() == "sell" {
			if user_trade.Side.String() == "buy" {
				return user_trade, nil
			}
		} else {
			if user_trade.Side.String() == "sell" {
				return user_trade, nil
			}

		}
	}
	return empty_ut, nil
}

func (bot *TradingBot) getWeightedBidAskSpread() (weighted_spread float64) {
	switch {
	case bot.CountCancelationOrders >= 3 && bot.CountCancelationOrders < 6:
		return 0.01 * bot.getBidAskSpread()
	case bot.CountCancelationOrders >= 6 && bot.CountCancelationOrders < 8:
		return 0.015 * bot.getBidAskSpread()
	case bot.CountCancelationOrders >= 8 && bot.CountCancelationOrders < 10:
		return 0.025 * bot.getBidAskSpread()
	case bot.CountCancelationOrders >= 10:
		return 0.05 * bot.getBidAskSpread()
	default:
		return 0.0
	}
}

func (bot *TradingBot) getBidAskSpread() (spread float64) {
	ticker, err := bot.BitsoClient.Ticker(&bot.BitsoBook) // btc_mxn
	if err != nil {
		log.Fatalln("error getting ticket request")
	}
	return ticker.Ask.Float64() - ticker.Bid.Float64()
}

func (bot *TradingBot) getTradingLowerLimit() float64 {
	return bot.MinMinorAmountToTrade * 0.15
}

func (bot *TradingBot) getTradingUpperLimit() float64 {
	return bot.MinMinorAmountToTrade * 0.01
}

func (bot *TradingBot) checkTotalAndRoundedTotal(total, rounded_total float64) float64 {
	temp := rounded_total

	for {
		if rounded_total > total {
			temp = rounded_total
			return temp
		}

		if rounded_total == total {
			rounded_total += 10
		}
	}
}

func stopSimulation() {
	log.Fatalln("Simulation stopped")
}
