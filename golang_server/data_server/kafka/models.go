package kafka

import (
	"github.com/xiam/bitso-go/bitso"
)

type ClientTickerParser struct {
	Ticker     *bitso.Ticker
	SCreatedAt string `json:"screated_at"`
}
