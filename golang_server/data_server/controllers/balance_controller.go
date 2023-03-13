package controllers

import (
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/xiam/bitso-go/bitso"
)

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 4, 4, 3, ' ', 0)
}

// Highest price a buyer is able to buy
func GetClientBalance(client *bitso.Client) {
	balances, err := client.Balances(nil)
	if err != nil {
		log.Fatal("client.Fundings: ", err)
	}

	for _, balance := range balances {
		log.Print(balance)
	}
}

func PrintBalance(client *bitso.Client) {
	balances, err := client.Balances(nil)
	if err != nil {
		log.Fatal("client.Balances: ", err)
	}
	w := newTabWriter()
	fmt.Fprintf(w, "CURRENCY\tTOTAL\tLOCKED\tAVAILABLE\n")
	for _, b := range balances {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			strings.ToUpper(b.Currency.String()),
			b.Total,
			b.Locked,
			b.Available,
		)
	}

	w.Flush()
}
