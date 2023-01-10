package repository

import (
	"github.com/xiam/bitso-go/bitso"
)

// UserRepository is the interface User CRUD
type CassandraRepository interface {
	SaveTicker(ticker bitso.Ticker) error
}
