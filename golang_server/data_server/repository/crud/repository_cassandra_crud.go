package crud

import (
	"github.com/gocql/gocql"
	"github.com/xiam/bitso-go/bitso"
	"github.com/xiam/bitso-go/utils/channels"
	// "github.com/go-sql-driver/mysql"
)

var rs *gocql.Conn

// CassandraRepositoryCRUD is the struct for the User CRUD
type CassandraRepositoryCRUD struct {
	session *gocql.Session
}

// NewCassandraRepositoryCRUD returns a new repository with DB connection
func NewCassandraRepositoryCRUD(session *gocql.Session) *CassandraRepositoryCRUD {
	return &CassandraRepositoryCRUD{session}
}

// Save returns a new user created or an error
func (s *CassandraRepositoryCRUD) SaveTicker(ticker bitso.Ticker) error {
	var err error
	done := make(chan bool)
	go func(ch chan<- bool) {
		defer close(ch)
		err := s.session.Query(`INSERT INTO tickers (id, ask, bid, book, created_at, high,last, low, volume, vwap, VALUES (?, ?, ?,?, ?, ?,?, ?, ?, ?)`,
			gocql.TimeUUID(), ticker.Ask, ticker.Bid, ticker.Book, ticker.CreatedAt, ticker.High, ticker.Last, ticker.Low, ticker.Volume, ticker.Vwap).Exec()
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
