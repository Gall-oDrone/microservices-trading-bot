package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/rs/cors"
	"github.com/xiam/bitso-go/config"
	"github.com/xiam/bitso-go/router"
)

// Run message
func Run() {
	config.Load() // Loads server port do not delete
	fmt.Printf("\n\tListening [::]:%d\n", config.PORT)
	// controllers.SocketHandler(nil, nil)
	//kafka.initProducer()
	listen(config.PORT)
}

func listen(port int) {
	r := router.New()
	corsWrapper := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "HEAD", "POST", "PUT", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Origin", "Accept", "X-Requested-With", "*"},
	}).Handler(r)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), corsWrapper))
}
