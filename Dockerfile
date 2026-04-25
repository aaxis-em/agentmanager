FROM golang:1.22

WORKDIR /usr/src/app

RUN go get github.com/gorilla/websocket
COPY . .

CMD ["go","run","main.go"]
