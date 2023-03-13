package controllers

import (
	"log"

	"github.com/xiam/bitso-go/bitso"
)

// Highest price a buyer is able to buy
func GetClientFundings(client *bitso.Client) {
	fundings, err := client.Fundings(nil)
	if err != nil {
		log.Fatal("client.Fundings: ", err)
	}

	for _, funding := range fundings {
		log.Print(funding)
	}
}
