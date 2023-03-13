#####################################
#   STEP 1 build executable binary  #
#####################################
FROM golang:alpine AS builder
ENV GO111MODULE=on

# Install git.
# Git is required for fetching the dependencies.
RUN apk update && apk add --no-cache git && apk add --no-cache bash

#RUN go get github.com/golang/snappy
WORKDIR /app

COPY go.mod .
COPY go.sum .

# RUN go install github.com/segmentio/kafka-go@v0.4.28
RUN go mod download
RUN go mod tidy

COPY . .

RUN go get github.com/segmentio/kafka-go
# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o main

EXPOSE 8080
#####################################
#   STEP 2 build a small image      #
#####################################
FROM scratch

# Copy our static executable.
COPY --from=builder /app/main /app/main

# Run the hello binary.
ENTRYPOINT ["/app/main"]