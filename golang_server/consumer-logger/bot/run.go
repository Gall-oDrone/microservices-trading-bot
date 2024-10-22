package bot

import (
	"log"
	"os"
	"time"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
)

const (
	MaxActiveOrders = 1
	DEBUG           = false
	STAGE           = false
)

var (
	Major bitso.Currency
	Minor bitso.Currency
	env   interface{}
)

func Run() {
	bot.setInitialConfg()
	bot.startTrading()
	setTimer()

}

func (bot *TradingBot) setInitialConfg() {
	bitso_client := bitso.NewClient(nil)
	setBitsoAPIClientConfig(bitso_client)
	dbClient := setDBClient()

	setCurrencies()
	book := bitso.NewBook(Major, Minor)
	order_side := bitso.OrderSide(1)
	bid_first := true
	bot := NewTradingBot(bitso_client, dbClient, order_side, book, bid_first, DEBUG, STAGE)
	if DEBUG {
		env = TestingBehavior{bot: bot}
	} else {
		env = ProductionBehavior{bot: bot}
		env.setKafkaClient()
		env.setInitBalance(dbClient)
		env.checkInitCurrencyBalance(Major, Minor, dbClient)
		env.setInitExchangeOrderBooks(dbClient)
		env.setInitFees(dbClient)
		env.clearRedisWsBatchTradeIdsList(dbClient)
		env.clearRedisUserOrders(dbClient)
		env.setInitBotConfig()
	}
}

func setBitsoAPIClientConfig(bc *bitso.Client) {
	var (
		key    string
		secret string
	)
	if STAGE {
		key = os.Getenv("STAGE_BITSO_API_KEY")
		secret = os.Getenv("STAGE_BITSO_API_SECRET")
	} else {
		key = os.Getenv("BITSO_API_KEY")
		secret = os.Getenv("BITSO_API_SECRET")
	}
	if len(key) > 0 && len(secret) > 0 {
		bc.key = key
		bc.secret = secret
	} else {
		log.Fatalln("error encounter during bitso client config")
	}
}

func setDBClient() *database.DatabaseClient {
	dbClient, err := database.CreateDatabaseClient("redis")
	if err != nil {
		log.Fatalf("Error creating database client: %v", err)
	}
	return dbClient
}

func setCurrencies() {
	Major = bitso.ToCurrency("btc")
	Minor = bitso.ToCurrency("mxn")
}

func setTradingTimer() {
	// Run the bot for a certain period
	endTime := time.Now().Add(12 * time.Hour) // Running the bot For 12 hours
	for time.Now().Before(endTime) {
		log.Println("Simulation started at:", time.Now())
		log.Println("Simulation will end at:", endTime)
		// Sleep for a random duration between trades
		log.Println("Simulation now slepping!")
		sleepDuration := 1 * time.Minute
		time.Sleep(sleepDuration)
	}
}
