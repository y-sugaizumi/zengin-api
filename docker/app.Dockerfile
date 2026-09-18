# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS builder
WORKDIR /src

# 依存関係だけを先にコピーしてレイヤーキャッシュを効かせる(このアプリは標準ライブラリのみで
# go.sum は無いが、go.mod だけでも変更検知の単位として分けておく)。
COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/zengin-api ./cmd/zengin-api

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates \
    && addgroup -g 1000 appuser \
    && adduser -D -H -u 1000 -G appuser appuser

WORKDIR /app
COPY --from=builder /out/zengin-api /usr/local/bin/zengin-api
COPY docker/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh \
    && mkdir -p /app/data \
    && chown -R appuser:appuser /app

ENV DATA_DIR=/app/data
USER appuser
EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["serve"]
