package main_test

import (
	"fmt"
	"github.com/xiam/bitso-go/router"
	"log"
	"net/http"
	// "github.com/rs/cors"
)

func main_test() {

	r := router.Router()
	fmt.Println("Starting server on the port 8080...")
	log.Fatal(http.ListenAndServe(":8080", r))

	// corsWrapper := cors.New(cors.Options{
	// 	AllowedMethods: []string{"GET", "POST"},
	// 	AllowedHeaders: []string{"Content-Type", "Origin", "Accept", "*"},
	// })
}
