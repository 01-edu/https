FROM docker.01-edu.org/golang:1.16.3-alpine3.13 as builder

ENV GIT_TERMINAL_PROMPT=0
RUN apk add --no-cache git

WORKDIR /app
COPY go.* ./
RUN go mod download
COPY *.go ./
RUN go build

FROM docker.01-edu.org/caddy:2.4.6-alpine

RUN apk add --no-cache curl tzdata

ENTRYPOINT ["/app/main"]

COPY certs .
COPY development.tmpl production.tmpl ./
COPY --from=builder /app/main /app/main
