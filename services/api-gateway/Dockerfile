FROM golang:1.21-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum
COPY services/api-gateway/go.mod services/api-gateway/go.sum ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
RUN go mod download

# Copy service source code
COPY services/api-gateway .
RUN go build -o api-gateway ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/api-gateway .

CMD ["./api-gateway"]
