FROM golang:1.21

ADD . /app

WORKDIR /app

RUN GOOS=linux GOARG=amd64 go build -o app

CMD ["/app/app"]
