FROM golang:1.19-alpine3.16 as builder

WORKDIR /instance-terminator
COPY ./go.mod .
COPY ./go.sum .
RUN go mod download

COPY . .

RUN go build -o app ./cmd

FROM alpine:3.16
COPY --from=builder /instance-terminator/app .
CMD ./app
