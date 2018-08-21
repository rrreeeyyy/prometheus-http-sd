FROM golang:1.10 AS builder

WORKDIR /go/src/github.com/rrreeeyyy/prometheus-http-sd

COPY vendor vendor/
COPY main.go .

RUN env GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o /prometheus-http-sd main.go

FROM alpine:edge
RUN apk add --update --no-cache ca-certificates
COPY --from=builder /prometheus-http-sd /prometheus-http-sd
USER nobody
ENTRYPOINT ["/prometheus-http-sd"]
