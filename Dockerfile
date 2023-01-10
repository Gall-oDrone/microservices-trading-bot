FROM golang:1.19-alpine

# Adding git, bash and openssh to the image...
RUN apk update && apk upgrade && \
    apk add --no-cache bash git openssh

LABEL maintainer="DG <piehavok@hotmail.com>"

WORKDIR /app

COPY go.mod go.sum ./data_server/

RUN go mod download

COPY . .

RUN go bulid -o main .

EXPOSE 8080

CMD ["./main"]