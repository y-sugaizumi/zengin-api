#!/bin/sh
set -eu

DATA_DIR="${DATA_DIR:-/app/data}"
role="${1:-serve}"

case "$role" in
  serve)
    if [ ! -f "${DATA_DIR}/banks.json" ]; then
      echo "[entrypoint] データが存在しません。初回取得を実行します..."
      zengin-api update
    fi
    exec zengin-api serve
    ;;
  update)
    exec zengin-api update
    ;;
  healthcheck)
    exec zengin-api healthcheck
    ;;
  *)
    exec "$@"
    ;;
esac
