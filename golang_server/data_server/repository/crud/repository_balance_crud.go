package crud

import (
	"github.com/xiam/bitso-go/bitso"
	"github.com/xiam/bitso-go/utils/channels"

	// "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var r *gorm.DB

// RepositoryBalanceCRUD is the struct for the User CRUD
type RepositoryBalanceCRUD struct {
	db *gorm.DB
}

// NewRepositoryBalanceCRUD returns a new repository with DB connection
func NewRepositoryBalanceCRUD(db *gorm.DB) *RepositoryBalanceCRUD {
	return &RepositoryBalanceCRUD{db}
}

// Save returns a new user created or an error
func (r *RepositoryBalanceCRUD) Save(balance bitso.Balance) error {
	var err error
	done := make(chan bool)
	go func(ch chan<- bool) {
		defer close(ch)
		if err != nil {
			ch <- false
			return
		}
		ch <- true
	}(done)
	if channels.OK(done) {
		return nil
	}
	return err
}

// Delete removes an user from the DB
func (r *RepositoryBalanceCRUD) Delete(uid uint32) (int64, error) {
	var err error
	done := make(chan bool)
	go func(ch chan<- bool) {
		defer close(ch)
		ch <- true
	}(done)

	if channels.OK(done) {
		if err != nil {
			return 0, err
		}

		return 1, nil
	}
	return 0, err
}
