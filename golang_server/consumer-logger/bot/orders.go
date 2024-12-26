package bot

import (
	"errors"
	"log"

	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
	"github.com/segmentio/kafka-go/example/consumer-logger/database"
)

type BaseOrderBehavior struct {
	DBClient database.DatabaseClient
}

func (b *BaseOrderBehavior) HandleOrderCancelation(bot *TradingBot, oid string) error {
	if len(oid) == 0 {
		return errors.New("Order ID is an empty string!")
	}
	bot.mutex.Lock()
	openOrders, err := bot.getBitsoUserOpenOrders()
	if err != nil {
		return errors.New("error while querying bitso open orders request: ", err)
	}
	for _, openOrder := range openOrders {
		if openOrder.OID == oid {
			if openOrder.Status == bitso.OrderStatus(1) {
				res, err := bot.BitsoClient.CancelOrder(oid)
				if err != nil {
					return errors.New("error while canceling order request: ", err)
				}
				b.RemoveOrderFromQueue(oid)
				log.Println("order cancelation request successfully fulfilled: ", res)
			}
		}
	}
	return nil
}

func (b *BaseOrderBehavior) HandleCompletedOrder(bot *TradingBot, os, oid string) (bool, error) {
	order_trades, err := bot.getBitsoOrderTrades(oid)
	if err != nil {
		return false, err
	}
	for _, order_trade := range order_trades {
		if order_trade.oid.String() == oid {
			return true, nil
		}
	}
	return false, nil
}

func (b *BaseOrderBehavior) HandleQueuedOrder(bot *TradingBot, os, oid string) error {
}

func (b *BaseOrderBehavior) HandlePartiallyFilledOrder(bot *TradingBot, os, oid string) (bool, error) {
	user_orders, err := bot.getBitsoLookUpOrders(oid)
	if err != nil {
		return false, err
	}
	for _, order := range user_orders {
		if order.OId.String() == oid && order.Status.String() == os {
			return true, nil
		}
	}
	return false, err
}

func (b *BaseOrderBehavior) HandleOpenOrder(bot *TradingBot, os, oid string) (bool, error) {
	user_orders, err := bot.getBitsoLookUpOrders(oid)
	if err != nil {
		return false, err
	}
	for _, order := range user_orders {
		if order.OId.String() == oid && order.Status.String() == os {
			return true, nil
		}
	}
	return false, err
}

func (b *BaseOrderBehavior) HandleCanceledOrder(bot *TradingBot, oid string) error {
	_, err := bot.BitsoCancelOrder(oid)
	return err
}
func (b *BaseOrderBehavior) HandleOrderStatus(bot *TradingBot, os, oid string) error {
	if len(oid) == 0 {
		return errors.New("Order ID is an empty string!")
	}
	switch os {
	case "completed":
		return b.HandleCompletedOrder(bot, os, oid)
	case "canceled":
		return b.HandleCanceledOrder(bot, oid)
	case "partially_filled":
		return b.HandlePartiallyFilledOrder(bot, os, oid)
	default:
		return b.HandleOpenOrder(bot, os, oid)
	}
}
