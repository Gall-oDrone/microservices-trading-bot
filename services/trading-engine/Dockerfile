FROM golang:1.21-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum
COPY services/trading-engine/go.mod services/trading-engine/go.sum ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
RUN go mod download

# Copy service source code
COPY services/trading-engine .
RUN go build -o trading-engine ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/trading-engine .

CMD ["./trading-engine"]
