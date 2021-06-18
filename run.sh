#!/usr/bin/env bash

set -euo pipefail
IFS='
'
cd -P "$(dirname "$0")"

DOMAIN=${1:-dev.01-edu.org}

if test "$(dig +short "$DOMAIN")" = "127.0.0.1"; then
    mkcert \
        -cert-file="${DOMAIN}-cert.pem" \
        -key-file="${DOMAIN}-key.pem" \
        "${DOMAIN}" \
        "git.${DOMAIN}"
fi

docker build -t docker.01-edu.org/https .
docker container rm --force https 2>/dev/null
docker volume rm caddy_config 2>/dev/null ||:
docker volume rm caddy_data 2>/dev/null ||:
docker network create endpoint 2>/dev/null ||:
docker run \
    --detach \
    --name https \
    --network endpoint \
    --restart unless-stopped \
    --volume /var/run/docker.sock:/var/run/docker.sock:ro \
    --volume caddy_config:/config \
    --volume caddy_data:/data \
    --publish 80:80 \
    --publish 443:443 \
    docker.01-edu.org/https
