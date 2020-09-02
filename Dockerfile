FROM golang:1.14.6-buster as base

ENV GO111MODULE=auto
WORKDIR /app

FROM base as modules

COPY go.mod go.sum ./
RUN go mod download && go mod tidy

FROM modules as build

COPY *.go /app/
RUN go build

FROM modules as dev

RUN go get golang.org/x/tools/gopls@latest

FROM build as test

CMD ["go", "test"]
