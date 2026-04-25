FROM golang:1.22

WORKDIR /usr/src/app

##RUN go get github.com/gorilla/websocket
COPY . .
EXPOSE 8080
CMD ["go","run","main.go"]
