package routes

import (
	"net/http"

	"github.com/xiam/bitso-go/controllers"
)

var kafkaRoutes = []Route{
	Route{
		URI:          "/kafka-producer",
		Method:       http.MethodGet,
		Handler:      controllers.ProducerHandler,
		AuthRequired: false,
	},
	Route{
		URI:          "/kafka-producer2",
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
