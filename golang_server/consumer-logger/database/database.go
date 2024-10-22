package database

import (
	"errors"
	"fmt"
)

// DatabaseClient defines the behavior expected from all database clients
type DatabaseClient interface {
	Connect() error // Method to connect to the database
	Close() error   // Method to close the connection
	// Add other common methods here, such as Get, Set, Query, etc.
}

// CreateDatabaseClient initializes and returns the correct database client based on the type.
func CreateDatabaseClient(dbType string) (*DatabaseClient, error) {
	switch dbType {
	case "redis":
		return RedisNewClient(), nil
	// Add cases for other databases (e.g., MySQL, PostgreSQL, etc.)
	default:
		return nil, errors.New(fmt.Sprintf("Unsupported database type: %s", dbType))
	}
}
