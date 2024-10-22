package bot

import (
	"log"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/table"
)

type TradingBot struct {
	BitsoClient                         *bitso.Client
	DBClient                            *database.DatabaseClient
	KafkaClient                         *queue.KafkaClient
	BitsoBook                           bitso.Book
	BitsoExchangeBook                   bitso.ExchangeOrderBook
	BitsoOId                            string
	BitsoLastCompleteOId                string
	BitsoActiveOIds                     []string
	MajorActiveOrders                   []string
	MinorActiveOrders                   []string
	Side                                bitso.OrderSide
	BidFirst                            bool
	Threshold                           float64
	OrderSet                            chan bool
	MajorProfit                         TradingProfit
	MinorProfit                         TradingProfit
	MaxPublicAPIRequestsPerMinute       int
	MaxPrivateAPIRequestsPerMinute      int
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
	MinMinorAllowToTrade                float64
	MaxMinorAllowToTrade                float64
	MinMajorAllowToTrade                float64
	MaxMajorAllowToTrade                float64
	TradingFrequency                    int8
	TableData                           *table.TableData
}

func NewTradingBot(b_client *bitso.Client, db_client interface{}, side bitso.OrderSide, book *bitso.Book, bidFirst, debug, stage bool) *TradingBot {
	return &TradingBot{
		BitsoClient:                    b_client,
		DBClient:                       db_client,
		BitsoBook:                      book,
		MaxPublicAPIRequestsPerMinute:  60,
		MaxPrivateAPIRequestsPerMinute: 300,
		Side:                           side,
		BidFirst:                       bidFirst,
		Threshold:                      0.1,
		Debug:                          debug,
		Stage:                          stage,
	}
}

func (bot *TradingBot) getTicker(book *bitso.Book) (*bitso.Ticker, error) {
	ticker, err := bot.BitsoClient.Ticker(&book)
	if err != nil {
		return nil, err
	}
	bot.updatePublicAPIRequestPerMinute()
	return ticker, nil
}

func (bot *TradingBot) getMinMinorValue() float64 {
	eo_book := bot.BitsoExchangeBook
	return eo_book.MinimumValue.Float64()
}

func (bot *TradingBot) checkMinMinorValue(value float64) bool {
	eo_book := bot.BitsoExchangeBook
	if value < eo_book.MinimumValue.Float64() {
		log.Panicf("minor value to buy is less than minimum value to trade.")
		return false
	}
	return true
}

func (bot *TradingBot) checkMaxMinorValue(value float64) bool {
	eo_book := bot.BitsoExchangeBook
	if value > eo_book.MaximumValue.Float64() {
		log.Panicf("minor value to buy is more than maximum value to trade.")
		return false
	}
	return true
}

func (bot *TradingBot) getMinMajorAmount() float64 {
	eo_book := bot.BitsoExchangeBook
	return eo_book.MinimumAmount.Float64()
}

func (bot *TradingBot) setTradingFrequency() {
	// TODO Implement a formula based on orders completed
	bot.TradingFrequency = 5
}

func (bot *TradingBot) setMinMinorAllowToTrade() {
	spread := bot.getSpread()
	value := (spread * 0.15) + bot.getMinMinorValue()
	if bot.checkMinMinorValue(value) {
		bot.MinMinorAllowToTrade = value
	} else {
		log.Panicf("the value %v is less than the minimum minor to trade", value)
	}
}

func (bot *TradingBot) setMaxMinorAllowToTrade() {
	spread := bot.getSpread()
	value := spread + bot.getMinMinorValue()
	if bot.checkMinMinorValue(value) {
		bot.MaxMinorAllowToTrade = value
	} else {
		log.Panicf("the value %v is more than the maximum minor to trade", value)
	}
}

func (bot *TradingBot) setTableObject() {
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

func (bot *TradingBot) getFee() bitso.Currency {

	fee, err := bot.DBClient.GetFee(bot.BitsoBook)
	if err != nil {
		log.Fatalln("error getting fee: ", err)
	}

	return fee
}

func (bot *TradingBot) postOrderId(orderId string, side bitso.OrderSide, orderStatus bitso.OrderStatus) error {

	err := bot.DBClient.PostOrder(bot.BitsoBook.String(), orderId, side.String(), orderStatus.String())
	if err != nil {
		log.Panicln("error posting order id: ", err)
		return err
	}
	return nil
}

func (bot *TradingBot) getOrderIdsByKey(key, value string) (oids map[string]string, err error) {
	switch key {
	case "status":
		oids, err := bot.DBClient.GetOrdersByStatus(value)
		if err != nil {
			log.Panicln("error getting order ids by side: ", err)
		}
	default:
		oids, err := bot.DBClient.GetOrdersBySide(value)
		if err != nil {
			log.Panicln("error getting order ids by side: ", err)
		}
	}
	return oids, err
}

func (bot *TradingBot) updatePublicAPIRequestPerMinute() {
	bot.handleMaxPublicAPIRequestPerMinute()
	bot.MaxPublicAPIRequestsPerMinute--
}

func (bot *TradingBot) updatePrivateAPIRequestPerMinute() {
	bot.handleMaxPrivateAPIRequestPerMinute()
	bot.MaxPrivateAPIRequestsPerMinute--
}

func (bot *TradingBot) handleMaxPublicAPIRequestPerMinute() {
	sleepDuration := 1 * time.Hour
	if bot.MaxPublicAPIRequestsPerMinute == 0 {
		log.Printf("Max Public API requests reached!, bot will sleeping %v hours \n", sleepDuration)
		time.Sleep(sleepDuration)
		log.Println("Max Public API requests restored!, bot will keep trading")
	}
}

func (bot *TradingBot) handleMaxPrivateAPIRequestPerMinute() {
	sleepDuration := 1 * time.Hour
	if bot.MaxPrivateAPIRequestsPerMinute == 0 {
		log.Printf("Max Private API requests reached!, bot will sleeping %v hours \n", sleepDuration)
		time.Sleep(sleepDuration)
		log.Println("Max Private API requests restored!, bot will keep trading")
	}
}

// resetAPIRequests resets the API request counters every minute
func (bot *TradingBot) resetAPIRequests() {
	ticker := time.NewTicker(1 * time.Minute) // Ticker for every minute
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Reset the counters
			bot.MaxPublicAPIRequestsPerMinute = 60
			bot.MaxPrivateAPIRequestsPerMinute = 300
			// Optionally log the reset
			// fmt.Println("API request limits reset to 0")
		}
	}
}

func (bot *TradingBot) getSpread() float64 {
	ticker := bot.BitsoClient.Ticker(bot.BitsoBook)
	bot.updatePublicAPIRequestPerMinute()
	return (ticker.Ask.Float64() - ticker.Bid.Float64())
}
