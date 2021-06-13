#!/usr/bin/env bash

set -euo pipefail
IFS='
'
cd -P "$(dirname "$0")"

DOMAIN=${1:-dev.01-edu.org}
mkcert \
    -cert-file="${DOMAIN}-cert.pem" \
    -key-file="${DOMAIN}-key.pem" \
    "${DOMAIN}" \
    "git.${DOMAIN}"
