package routes

import (
	"net/http"

	"github.com/xiam/bitso-go/controllers"
)

var socketRoutes = []Route{
	Route{
		URI:          "/socket",
		Method:       http.MethodGet,
		Handler:      controllers.SocketHandler,
		AuthRequired: false,
	},
}
