FROM docker.01-edu.org/golang:1.16.3-alpine3.13 as builder

ENV GIT_TERMINAL_PROMPT=0
RUN apk add --no-cache git

WORKDIR /app
COPY go.* ./
RUN go mod download
COPY *.go ./
RUN go build

FROM docker.01-edu.org/caddy:2.3.0-alpine

RUN apk add --no-cache curl tzdata
RUN apk add --no-cache iproute2

ENTRYPOINT ["/app/main"]

COPY *.pem config.json ./
COPY config config
COPY --from=builder /app/main /app/main
