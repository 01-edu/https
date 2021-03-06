#!/usr/bin/env bash

set -euo pipefail
IFS='
'
cd -P "$(dirname "$0")"

DOMAIN=${DOMAIN:-dev.01-edu.org}

if test "$(dig +short "$DOMAIN")" = "127.0.0.1"; then
    mkcert -cert-file "certs/${DOMAIN}-cert.pem"     -key-file "certs/${DOMAIN}-key.pem"     "${DOMAIN}"
fi

docker build -t https .
docker container rm --force https 2>/dev/null
docker volume rm https_config 2>/dev/null ||:
docker volume rm https_data 2>/dev/null ||:
docker network create https 2>/dev/null ||:
docker run \
    --detach \
    --name https \
    --network https \
    --restart unless-stopped \
    --volume /var/run/docker.sock:/var/run/docker.sock:ro \
    --volume https_config:/config \
    --volume https_data:/data \
    --publish 80:80 \
    --publish 443:443 \
    https ${1:-}
