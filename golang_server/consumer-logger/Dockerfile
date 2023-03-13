#####################################
#   STEP 1 build executable binary  #
#####################################
FROM golang:alpine AS builder

ENV GO111MODULE=on
# ENV CGO_ENABLED=0
# ENV GOOS=linux
# ENV GOARCH=amd64
# ENV GOFLAGS=-mod=vendor
# ENV APP_USER app
# ENV APP_HOME /go/src/producer-random-home

# Install git.
# Git is required for fetching the dependencies.
RUN apk update && apk add --no-cache git

WORKDIR /app

COPY go.mod .
COPY go.sum .

COPY . .

RUN go mod download
RUN go mod tidy
# Build the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o main

#####################################
#   STEP 2 build a small image      #
#####################################
FROM scratch

# Copy our static executable.
COPY --from=builder /app/main /app/main

# Run the hello binary.
ENTRYPOINT ["/app/main"]