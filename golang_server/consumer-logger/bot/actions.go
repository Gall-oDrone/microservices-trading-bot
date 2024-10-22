package bot

import (
	"errors"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
	"github.com/segmentio/kafka-go/example/consumer-logger/table"
	"github.com/segmentio/kafka-go/example/consumer-logger/utils"
)

type SideBehavior interface {
	CheckFunds(min float64) error
	HandleOrderRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Monetary, market_trade string, tableData *table.TableData) (float64, error)
	SetOrderPlacement(book bitso.Book, order_type bitso.OrderType, amount, rate float64)
	HandleOrderPlacement(ticker *bitso.Ticker, amount, rate float64, oid string, order_status bitso.OrderStatus, order_type bitso.OrderType, redis_client *database.RedisClient) (string error)
	HandleOrderMaker() error
	UpdateOrdersQueue() error
}

type SellBehavior struct {
	MajorBalance        bitso.Balance
	CurrencyAmount      float64
	BitsoOrderPlacement *bitso.OrderPlacement
	Side                bitso.OrderSide
	FirstOrder          bool
	FirstTrade          bool
	OpenMajorOrders     []string
}

func InitSellBehavior(balance bitso.Balance) *SellBehavior {
	return &SellBehavior{
		MajorBalance: balance,
		Side:         bitso.OrderSide(1),
		FirstOrder:   true,
		FirstTrade:   true,
	}
}

func (s *SellBehavior) CheckFunds(min float64) error {
	if s.MajorBalance.Available.Float64() < min {
		return fmt.Errorf("No funds available to sell: %s", s.MajorBalance.Currency.String())
	}
	return nil
}

func (s *SellBehavior) HandleOrderRate(ticker *bitso.Ticker, limit, amount float64, fee bitso.Currency, market_trade string, tableData *table.TableData) (float64, error) {
	var (
		total     float64 // Order total in minor
		mvalue    float64 // Market value in minor
		ovalue    float64 // Order value (Ask) minor
		taker_fee float64 // In minor
		maker_fee float64 // In minor
		rpp       float64 // Rate percentage change
	)
	rateObj, err := InitRate(ticker.Bid.Float64(), ticker.Ask.Float64(), amount)
	if err != nil {
		return 0.0, err
	}
	mvalue = rateObj.CalculateValue(s.Side)
	if market_trade == "taker" {
		taker_fee = mvalue * fee.TakerFeeDecimal.Float64()
		total = rateObj.CalculateTotal(amount, mvalue, taker_fee, limit, s.Side)
	} else {
		maker_fee = (mvalue * fee.MakerFeeDecimal.Float64())
		total = rateObj.CalculateTotal(amount, mvalue, maker_fee, limit, s.Side)
	}
	ovalue = total * amount
	rpp = ((rateObj.BidRate / total) - 1) * 100
	tableData.GetTableOptimalPriceSummary("sell", amount, bid_rate, ovalue, taker_fee, maker_fee, total, rpp)
	if rpp > 0 {
		return utils.RoundToNearestTen(rateObj.BidRate)
	}
	// total -= (bot.getWeightedBidAskSpread())
	return utils.RoundToNearestTen(total)
}

func (s *SellBehavior) SetOrderPlacement(book bitso.Book, order_type bitso.OrderType, amount, rate float64) {
	s.BitsoOrderPlacement{
		Book:  book,
		Side:  s.Side(2),
		Type:  order_type,
		Major: bitso.ToMonetary(amount),
		Minor: "",
		Price: bitso.ToMonetary(rate),
	}
}

func (s *SellBehavior) HandleOrderPlacement(bc *bitso.Client, ticker *bitso.Ticker, book *bitso.Book, amount, rate float64, oid string, order_status bitso.OrderStatus, order_type bitso.OrderType, redis_client *database.RedisClient, tableData *table.TableData) (string, error) {
	log.Println("final order details")
	tableData.GetTableOrderPlacement(book, s.Side, order_type, bitso.ToMonetary(amount), "", bitso.ToMonetary(rate))
	log.Println("amount as minor (value): ", amount*rate)
	oid, err := bc.PlaceOrder(s.BitsoOrderPlacement)
	if err != nil {
		errorCode, min_amount_to_trade, _ := utils.ParseErrorMessage(err)
		if errorCode == 405 {
			log.Error(err)
			return s.HandleOrderPlacement(ticker, min_amount_to_trade, rate, oid, order_status, order_type, redis_client)
		}
		log.Error("Error placing ask order: ", err)
		return "", err
	}
	err = s.UpdateOrdersQueue(oid)
	if err != nil {
		return oid, err
	}
	return oid, nil
}

func (s *SellBehavior) UpdateOrdersQueue(oid string) error {
	if len(oid) == 0 {
		return errors.New("Order ID is an empty string!")
	}
	s.OpenMajorOrders = append(s.OpenMajorOrders, oid)
	return nil
}

func (s *SellBehavior) HandleOrderMaker(bot *TradingBot) error {
	var err error
	major_balance, err := bot.DBClient.GetUserBalance(bot.BitsoBook.Major().String())
	if err != nil {
		return err
	}
	market_trade := "maker"
	limit := (bot.MinMinorAllowToTrade() * 0.5)
	rate, err := s.HandleOrderRate(bot.getTicker(bot.BitsoBook), limit, bot.MinMinorAllowToTrade(), bot.getFee(), market_trade, bot.TableData)
	if err != nil {
		return err
	}
	value := bot.MinMinorAllowToTrade
	amount := bot.MinMinorAllowToTrade / bot.getTicker(bot.BitsoBook).Bid.Float64() // In BTC
	oid, err := s.HandleOrderPlacement(bot.BitsoClient, bot.getTicker(bot.BitsoBook), bot.BitsoBook, amount, rate, oid string, order_status bitso.OrderStatus, order_type bitso.OrderType, bot.DBClient, bot.TableData)
	if err != nil {
		return err
	}
}

/*
#################################################################################################################
#################################################################################################################
#################################################################################################################
*/
type BuyBehavior struct {
	MinorBalance   bitso.Balance
	CurrencyAmount float64
}

func InitBuyBehavior(balance bitso.Balance) *SellBehavior {
	return &SellBehavior{
		MinorBalance: balance,
		Side:         bitso.OrderSide(2),
		FirstOrder:   true,
		FirstTrade:   true,
	}
}

func (b *BuyBehavior) CheckFunds(min float64) error {
	if b.MinorBalance.Available.Float64() < min {
		return fmt.Errorf("No funds available to buy: %s", b.MinorBalance.Currency.String())
	}
	return nil
}
