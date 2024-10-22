package bot

import (
	"log"
	"math/rand"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
)

type TradingEnvironment interface {
	OrderMaker() error
	setInitBalance(redis_client *database.RedisClient)
}

type TestingBehavior struct {
	bot *TradingBot
}

func (tb *TestingBehavior) setInitBalance(redis_client *database.RedisClient) {
	tb.bot.MajorBalance = setBalance(0.0, 0.0, tb.bot.BitsoBook.Major.String())
	err := redis_client.SaveUserBalance(&tb.bot.MajorBalance)
	if err != nil {
		log.Fatal("Error saving user balance: ", err)
	}
	tb.bot.MinorBalance = setBalance(1000.0, 0.0, tb.bot.BitsoBook.Minor.String())
	err = redis_client.SaveUserBalance(&tb.bot.MinorBalance)
	if err != nil {
		log.Fatal("Error saving user balance: ", err)
	}
}

func (tb *TestingBehavior) OrderMaker() error {
	trade_rate_threshold := 0.5
	rand.Seed(time.Now().UnixNano())
	randomNumber := rand.Float64()
	oid := utils.GenRandomOId(7)
	rate := tb.bot.getOptimalPrice(ticker, market_trade, tb.bot.Side.String())
	if rate > 0 {
		if tb.bot.Side.String() == "buy" {
			if randomNumber < x {
				rate = ask_rate
				tb.bot.setUserTrade(rate, minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
			} else {
				tb.bot.setUserTrade(rate, minor_amount, book_fee.FeeDecimal, book, oid, redis_client)
			}
		} else {
			if randomNumber < x {
				rate = bid_rate
				tb.bot.setUserTrade(rate, major_amount, book_fee.FeeDecimal, book, oid, redis_client)
			} else {
				tb.bot.setUserTrade(rate, major_amount, book_fee.FeeDecimal, book, oid, redis_client)
			}
		}
	}
}

type ProductionBehavior struct {
	bot *TradingBot
}

func (pb *ProductionBehavior) setInitCurrencyBalance(dbClient *database.DatabaseClient) {
	balances, err := pb.bot.BitsoClient.Balances(nil)
	if err != nil {
		log.Fatal("Error requesting user balances: ", err)
	}
	pb.bot.updatePrivateAPIRequestPerMinute()
	for _, balance := range balances {
		if balance.Currency == pb.bot.BitsoBook.Major() {
			err = dbClient.PostUserBalance(&balance)
			if err != nil {
				log.Fatal("Error saving user balance: ", err)
			}
		} else if balance.Currency == pb.bot.BitsoBook.Minor() {
			err = dbClient.PostUserBalance(&balance)
			if err != nil {
				log.Fatal("Error saving user balance: ", err)
			}
		}
	}
}

func (pb *ProductionBehavior) checkInitCurrencyBalance(major, minor bitso.Currency, dbClient *database.DatabaseClient) {
	var (
		minor_balance bitso.Balance
		major_balance bitso.Balance
		err           error
	)
	minor_balance, err = dbClient.GetUserBalance(minor.String())
	if err != nil {
		log.Fatalln("error while getting minor balance. ", err)
	}
	major_balance, err = dbClient.GetUserBalance(major.String())
	if err != nil {
		log.Fatalln("error while getting major balance. ", err)
	}
	if pb.bot.Side == 1 && minor_balance.Available.Float64() == 0.0 { // bot trading behavior is first set to buy
		log.Fatalf("please add %v funds to this account", minor.String())
	} else if pb.bot.Side == 2 && major_balance.Available.Float64() == 0.0 {
		log.Fatalf("please add %v funds to this account", major.String())
	}
}

func (pb *ProductionBehavior) setInitExchangeOrderBooks(dbClient *database.DatabaseClient) {
	exchange_order_books, err := pb.bot.BitsoClient.AvailableBooks()
	if err != nil {
		log.Fatal("Error requesting exchange order books: ", err)
	}
	pb.bot.updatePublicAPIRequestPerMinute()
	err = dbClient.PostExchangeOrderBooks(exchange_order_books)
	if err != nil {
		log.Fatalln("Error saving fees: ", err)
	}
	pb.bot.BitsoExchangeBook, err = dbClient.GetExchangeOrderBook(pb.bot.BitsoBook)
	if err != nil {
		log.Fatalln("Error getting exchange order book: ", err)
	}
}

func (pb *ProductionBehavior) setInitFees(dbClient *database.DatabaseClient) {
	fees, err := pb.bot.BitsoClient.Fees(nil)
	if err != nil {
		log.Fatalln("Error during Fees request: ", err)
	}
	pb.bot.updatePublicAPIRequestPerMinute()
	err = dbClient.SaveFees(fees.Fees)
	if err != nil {
		log.Fatalln("Error saving fees: ", err)
	}
}

func (pb *ProductionBehavior) clearRedisWsBatchTradeIdsList(dbClient *database.DatabaseClient) {
	listName := "batchIdsUsed"
	keyPattern := "batchid_*"
	err := dbClient.ClearBatchIDsList(listName)
	if err != nil {
		log.Fatalln("error deleting unique batch ids list: ", err)
	}

	err = dbClient.DeleteKafkaBatchKeysByPattern(keyPattern)
	if err != nil {
		log.Fatalln("error deleting ws trade batches: ", err)
	}
	log.Println("successfully deleted all ws trade batch records from redis!")
}

func (pb *ProductionBehavior) clearRedisUserOrders(dbClient *database.DatabaseClient) {
	err := dbClient.DeleteAllUserOrders()
	if err != nil {
		log.Fatalln("error deleting ws trade batches: ", err)
	}
	log.Println("successfully deleted all user orders saved in redis!")
}

func (pb *ProductionBehavior) setInitBotConfig() {
	pb.bot.setTradingFrequency()
	pb.bot.setMinMinorAllowToTrade()
	pb.bot.setMaxMinorAllowToTrade()
	pb.bot.setTableObject()
}

func (pb *ProductionBehavior) setInitActions() {
	minor_balance, err := pb.bot.dbClient.GetUserBalance(pb.bot.BitsoBook.Minor().String())
	if err != nil {
		log.Fatalln("error while getting minor balance. ", err)
	}
	major_balance, err := pb.bot.dbClient.GetUserBalance(pb.bot.BitsoBook.Major().String())
	if err != nil {
		log.Fatalln("error while getting major balance. ", err)
	}
	sell := InitSellBehavior(major_balance)
	buy := InitBuyBehavior(minor_balance)
}

func (pb *ProductionBehavior) OrderMakerHandler(sell *SellBehavior) error {
	sell.HandleOrderMaker(pb.bot)
}
