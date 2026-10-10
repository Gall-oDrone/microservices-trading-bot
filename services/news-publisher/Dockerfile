FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY shared/ ./shared/
COPY services/news-publisher/ ./services/news-publisher/
WORKDIR /app/services/news-publisher
RUN go mod download && CGO_ENABLED=0 go build -o /news-publisher ./cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /news-publisher /usr/local/bin/news-publisher
EXPOSE 8090
ENTRYPOINT ["news-publisher"]
