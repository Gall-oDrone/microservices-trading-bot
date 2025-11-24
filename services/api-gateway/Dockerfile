FROM golang:1.24-alpine AS builder

WORKDIR /app
# Copy service-specific go.mod and go.sum (go.sum may not exist)
COPY services/api-gateway/go.mod ./
# Copy shared directory to match replace directive (../../shared from go.mod location)
# Since go.mod is at /app/go.mod, ../../shared means /shared
COPY shared /shared
# Conditionally copy go.sum if it exists (mount build context to access source files)
RUN --mount=type=bind,source=.,target=/context \
    test -f /context/services/api-gateway/go.sum && cp /context/services/api-gateway/go.sum ./go.sum || true
RUN go mod download

# Copy service source code
COPY services/api-gateway .
RUN go build -o api-gateway ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/api-gateway .

CMD ["./api-gateway"]
