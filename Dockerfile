FROM golang:alpine

RUN apk add --no-cache mysql-client

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o main . && chmod 777 locales

EXPOSE 8080

CMD ["./main"]
