module bitso-trading-platform/data-collector

go 1.21

require (
	bitso-trading-platform/shared v0.0.0
	github.com/aws/aws-sdk-go-v2 v1.32.6
	github.com/aws/aws-sdk-go-v2/config v1.28.6
	github.com/aws/aws-sdk-go-v2/service/s3 v1.71.0
	github.com/jackc/pgx/v5 v5.7.1
	github.com/joho/godotenv v1.5.1
	github.com/prometheus/client_golang v1.20.5
	github.com/xitongsys/parquet-go v1.6.2
)

replace bitso-trading-platform/shared => ../../shared
