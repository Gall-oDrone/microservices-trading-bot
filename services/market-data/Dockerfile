FROM golang:1.21-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum (go.sum exists for this service)
COPY services/market-data/go.mod ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
# Copy go.sum (it exists for market-data)
COPY services/market-data/go.sum ./
RUN go mod download

# Copy service source code
COPY services/market-data .
RUN go build -o market-data ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/market-data .

CMD ["./market-data"]
