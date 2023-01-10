package database

import (
	"log"
	"os"
	"time"

	"github.com/gocql/gocql"
)

var cluster *gocql.ClusterConfig

func setClusterConfig(cluster *gocql.ClusterConfig) {
	cluster.Consistency = gocql.Quorum
	cluster.ProtoVersion = 4
	cluster.ConnectTimeout = time.Second * 10
	cluster.Authenticator = gocql.PasswordAuthenticator{Username: os.Getenv("Cassandra_Username"), Password: os.Getenv("Cassandra_Password")}
}

func setDBSchema(cluster *gocql.ClusterConfig) {
	session, err := cluster.CreateSession()
	if err != nil {
		log.Println(err)
		return
	}
	defer session.Close()

	// create keyspaces
	err = session.Query("CREATE KEYSPACE IF NOT EXISTS crypto_trading WITH REPLICATION = {'class' : 'NetworkTopologyStrategy', 'AWS_VPC_US_WEST_2' : 3};").Exec()
	if err != nil {
		log.Println(err)
		return
	}

	// creates ticker table
	err = session.Query("CREATE TABLE IF NOT EXISTS crypto_trading.ticker (id UUID, ask double, bid double, book varchar, created_at timestamp,high double,last double,low double,volume double,vwap double, PRIMARY KEY (id, book, created_at));").Exec()
	if err != nil {
		log.Println(err)
		return
	}

	// creates orders table
	err = session.Query("CREATE TABLE IF NOT EXISTS crypto_trading.order (id UUID, ask boolean, bid boolean, book varchar, created_at timestamp,high double,last double,low double,volume double,vwap double, PRIMARY KEY (id, book, created_at));").Exec()
	if err != nil {
		log.Println(err)
		return
	}
}

func GetCluster() (cluster *gocql.ClusterConfig) {
	return cluster
}

func CassandraInitConnection() {
	cluster = gocql.NewCluster(os.Getenv("PublicIP"))
	setClusterConfig(cluster)
	setDBSchema(cluster)
}
