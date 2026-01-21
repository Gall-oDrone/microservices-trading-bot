module bitso-trading-platform/market-data

go 1.21

require bitso-trading-platform/shared v0.0.0

require github.com/joho/godotenv v1.5.1

require (
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/klauspost/compress v1.17.4 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/segmentio/kafka-go v0.4.47 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	golang.org/x/net v0.21.0 // indirect
	golang.org/x/sys v0.17.0 // indirect
)

replace bitso-trading-platform/shared => ../../shared
