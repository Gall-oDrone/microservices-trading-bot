package routes

import (
	"net/http"

	"github.com/xiam/bitso-go/controllers"
)

var registerRoutes = []Route{
	Route{
		URI:          "/api/signup",
		Method:       http.MethodPost,
		Handler:      controllers.SignUp,
		AuthRequired: false,
	},
	Route{
		URI:          "/api/validate/token/{token}",
		Method:       http.MethodGet,
		Handler:      controllers.TokenValidation,
		AuthRequired: false,
	},
	Route{
		URI:          "/api/validate/code/{code}",
		Method:       http.MethodGet,
		Handler:      controllers.CodeValidation,
		AuthRequired: false,
	},
}
