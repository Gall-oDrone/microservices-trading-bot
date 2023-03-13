package router

import (
	"github.com/xiam/bitso-go/router/routes"

	"github.com/gorilla/mux"
)

// New routes
func New() *mux.Router {
	// routers.WebsocketInit()
	r := mux.NewRouter().StrictSlash(true)
	return routes.SetupRoutesWithMiddlewares(r)
}
