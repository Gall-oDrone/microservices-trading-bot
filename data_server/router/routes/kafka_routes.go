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
}