FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY services/strategy-router/go.mod ./
COPY shared /shared
COPY services/strategy-router/go.sum ./
RUN go mod download

COPY services/strategy-router .
RUN go build -o strategy-router ./cmd/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/strategy-router .

EXPOSE 8092
CMD ["./strategy-router"]
