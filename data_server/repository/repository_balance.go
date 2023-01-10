package repository

import (
	"github.com/xiam/bitso-go/bitso"
)

// UserRepository is the interface User CRUD
type BalanceRepository interface {
	Save(bitso.Balance) error
	Delete(uint32) (int64, error)
}
