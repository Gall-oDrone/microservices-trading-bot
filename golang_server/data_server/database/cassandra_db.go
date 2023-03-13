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
	cluster.Authenticator = gocql.PasswordAuthenticator{
		Username: os.Getenv("CASSANDRA_USERNAME"), 
		Password: os.Getenv("CASSANDRA_PASSWORD"),
	}
}

func setDbSchema(cluster *gocql.ClusterConfig) {
	for {
		session, err := cluster.CreateSession()
		if err == nil {
			break
		}
		log.Printf("CreateSession: %v", err)
		// defer session.Close()
	
		// create keyspaces
		// err = session.Query("CREATE KEYSPACE IF NOT EXISTS crypto_trading WITH REPLICATION = {'class' : 'NetworkTopologyStrategy', 'AWS_VPC_US_WEST_2' : 3};").Exec()
		err = session.Query("CREATE KEYSPACE IF NOT EXISTS crypto_trading WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 1};").Exec()
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
    	time.Sleep(time.Second)
	}
	log.Printf("Connected OK")

}

func GetCluster() (cluster *gocql.ClusterConfig) {
	return cluster
}

func CassandraInitConnection() {
	// cluster = gocql.NewCluster(os.Getenv("HOSTS"))
	// setClusterConfig(cluster)
	// setDbSchema(cluster)
	cluster := gocql.NewCluster("cassandra")
    cluster.Authenticator = gocql.PasswordAuthenticator{
        Username: os.Getenv("CASSANDRA_USERNAME"),
        Password: os.Getenv("CASSANDRA_PASSWORD"),
    }

    for {
		_, err := cluster.CreateSession()
		if  err == nil {
			break
		}
		log.Printf("CreateSession: %v", err)
		time.Sleep(time.Second)
	}
	log.Printf("Connected OK")
}
