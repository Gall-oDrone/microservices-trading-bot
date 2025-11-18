FROM golang:1.21-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum
COPY services/backtesting/go.mod services/backtesting/go.sum ./
# Copy shared directory (needed for replace directive)
COPY shared ./shared
RUN go mod download

# Copy service source code
COPY services/backtesting .
RUN go build -o backtesting ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/backtesting .

CMD ["./backtesting"]
