module bitso-trading-platform/strategy-executor

go 1.23.0

toolchain go1.23.4

require (
	bitso-trading-platform/shared v0.0.0
	github.com/google/uuid v1.6.0
	github.com/redis/go-redis/v9 v9.5.1
	github.com/rs/zerolog v1.34.0
	github.com/segmentio/kafka-go v0.4.47
)

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/klauspost/compress v1.17.4 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)

replace bitso-trading-platform/shared => ../../shared
