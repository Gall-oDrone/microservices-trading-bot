package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// PORT server port
var (
	PORT      = 0
	SECRETKEY []byte
)

// Load the server PORT
func Load() {
	var err error
	err = godotenv.Load()
	if err != nil {
		log.Fatal("SIGMA", err)
	}
	PORT, err = strconv.Atoi(os.Getenv("API_PORT"))
	if err != nil {
		PORT = 9000
	}
}
