FROM golang:1.21-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum (go.sum exists for this service)
COPY services/order-management/go.mod ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
# Copy go.sum (it exists for order-management)
COPY services/order-management/go.sum ./
RUN go mod download

# Copy service source code
COPY services/order-management .
RUN go build -o order-management ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/order-management .

CMD ["./order-management"]
