package routes

import (
	"net/http"

	"github.com/xiam/bitso-go/controllers"
)

var kafkaRoutes = []Route{
	Route{
		URI:          "/bitso/ws/orders",
		Method:       http.MethodGet,
		Handler:      controllers.WsOrdersProducerHandler,
		AuthRequired: false,
	},
	Route{
		URI:          "/bitso/ws/trades",
		Method:       http.MethodGet,
		Handler:      controllers.WsTradesProducerHandler,
		AuthRequired: false,
	},
	Route{
		URI:          "/bitso/tickers",
		Method:       http.MethodGet,
		Handler:      controllers.ProducerHandler2,
		AuthRequired: false,
	},
	Route{
		URI:          "/kafka-producer3",
		Method:       http.MethodGet,
		Handler:      controllers.ProducerHandler3,
		AuthRequired: false,
	},
	Route{
		URI:          "/bitso/fees",
		Method:       http.MethodGet,
		Handler:      controllers.FeesHandler,
		AuthRequired: false,
	},
	Route{
		URI:          "/bitso/usertrades",
		Method:       http.MethodGet,
		Handler:      controllers.UserTradeHandler,
		AuthRequired: false,
	},
}
