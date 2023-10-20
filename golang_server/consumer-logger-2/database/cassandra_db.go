package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gocql/gocql"
	"github.com/segmentio/kafka-go/example/consumer-logger/bitso"
)

// var cluster *gocql.ClusterConfig

type CassandraClient struct {
	cluster *gocql.ClusterConfig
	session *gocql.Session
}

func NewCassandraClient(clusterHosts []string, keyspace string) (*CassandraClient, error) {
	var cs *gocql.Session
	cluster := gocql.NewCluster(clusterHosts...)
	cluster.Keyspace = keyspace
	cluster.Authenticator = gocql.PasswordAuthenticator{
		Username: os.Getenv("CASSANDRA_USERNAME"),
		Password: os.Getenv("CASSANDRA_PASSWORD"),
	}
	for {
		session, err := cluster.CreateSession()
		if err == nil {
			cs = session
			break
		}
		log.Printf("CreateSession: %v", err)
		time.Sleep(time.Second)
	}
	return &CassandraClient{cluster: cluster, session: cs}, nil
}

func (cc *CassandraClient) CloseDB() {
	if cc.session != nil {
		cc.session.Close()
		fmt.Println("Cassandra session closed")
	}
}

func setClusterConfig(cluster *gocql.ClusterConfig) {
	cluster.Consistency = gocql.Quorum
	cluster.ProtoVersion = 4
	cluster.ConnectTimeout = time.Second * 10
	cluster.Authenticator = gocql.PasswordAuthenticator{
		Username: os.Getenv("CASSANDRA_USERNAME"),
		Password: os.Getenv("CASSANDRA_PASSWORD"),
	}
}

func setDbSchema(session *gocql.Session) error {
	// err = session.Query("CREATE KEYSPACE IF NOT EXISTS crypto_trading WITH REPLICATION = {'class' : 'NetworkTopologyStrategy', 'AWS_VPC_US_WEST_2' : 3};").Exec()
	err := session.Query("CREATE KEYSPACE IF NOT EXISTS crypto_trading WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 1};").Exec()
	if err != nil {
		return err
	}

	// creates ticker table
	err = session.Query("CREATE TABLE IF NOT EXISTS crypto_trading.ticker (id UUID, ask double, bid double, book varchar, created_at timestamp,high double,last double,low double,volume double,vwap double, PRIMARY KEY (id, book, created_at));").Exec()
	if err != nil {
		return err
	}

	// creates orders table
	err = session.Query("CREATE TABLE IF NOT EXISTS crypto_trading.user_order (id UUID, ask boolean, bid boolean, book varchar, created_at timestamp,high double,last double,low double,volume double,vwap double, PRIMARY KEY (id, book, created_at));").Exec()
	if err != nil {
		return err
	}
	time.Sleep(time.Second)

	dropQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s;", "trades")
	if err := session.Query(dropQuery).Exec(); err != nil {
		return err
	}
	// creates trades table
	err = session.Query("CREATE TABLE IF NOT EXISTS crypto_trading.trades (book text, i bigint, a double, r double, v double, morder text, torder text, t int, x bigint, PRIMARY KEY (i));").Exec()
	if err != nil {
		return err
	}
	time.Sleep(time.Second)
	return nil
}

// GetCluster returns the Cassandra cluster configuration.
func (cc *CassandraClient) GetCluster() *gocql.ClusterConfig {
	return cc.cluster
}

// GetSession returns the Cassandra session.
func (cc *CassandraClient) GetSession() *gocql.Session {
	return cc.session
}

func CassandraInitConnection() (*CassandraClient, error) {
	// cluster = gocql.NewCluster(os.Getenv("HOSTS"))
	// Initialize the Cassandra client
	clusterHosts := []string{"cassandra"} // Cassandra cluster hosts
	keyspace := "crypto_trading"          // Your Cassandra keyspace
	cassandraClient, err := NewCassandraClient(clusterHosts, keyspace)
	if err != nil {
		log.Fatalf("Failed to initialize Cassandra client: %v", err)
		return nil, err
	}
	// defer cassandraClient.CloseDB()
	cluster := cassandraClient.GetCluster()
	session := cassandraClient.GetSession()

	// Truncate your table (e.g., "trades") when the server starts.
	tableName := "trades"
	setClusterConfig(cluster)
	if err := truncateTable(session, keyspace, tableName); err != nil {
		log.Fatal("error while truncating table: ", err)
		return nil, err
	}
	for {
		err := setDbSchema(session)
		if err == nil {
			break
		}
		log.Println("error while setting db schema: ", err)
	}
	return cassandraClient, nil
}

func (cc *CassandraClient) InsertTradeRecord(trades *bitso.WebsocketTrade) error {
	book := trades.Book.String()
	for _, trade := range trades.Payload {

		query := cc.session.Query(`INSERT INTO trades (book, i, a, r, v, morder, torder, t, x) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, book, trade.TID, trade.Amount.Float64(), trade.Price.Float64(), trade.Value.Float64(), trade.MakerOrderID, trade.TakerOrderID, trade.Side, trade.CreationTime)

		if err := query.Exec(); err != nil {
			log.Printf("Error inserting trade data: %v", err)
			return err
		}
	}
	fmt.Println("Trade data inserted successfully!")
	return nil
}

func truncateTable(session *gocql.Session, keyspaceName, tableName string) error {
	query := "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ? ALLOW FILTERING;"
	iter := session.Query(query, keyspaceName, tableName).Iter()

	// Check if the query returned any rows (table exists)
	if !iter.Scan(nil) {
		// Table does not exist
		return nil
	}

	// Table exists, so truncate it
	truncateQuery := fmt.Sprintf("TRUNCATE %s.%s;", keyspaceName, tableName)
	if err := session.Query(truncateQuery).Exec(); err != nil {
		return err
	}

	return nil
}
