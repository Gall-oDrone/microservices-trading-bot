FROM golang:1.23-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum (go.sum exists for this service)
COPY services/backtesting/go.mod ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
# Copy go.sum (it exists for backtesting)
COPY services/backtesting/go.sum ./
RUN go mod download

# Copy service source code
COPY services/backtesting .
RUN go build -o backtesting ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/backtesting .

CMD ["./backtesting"]
