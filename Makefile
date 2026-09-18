APP := zengin-api
GOLANG_BUILD_DIR ?= $(HOME)/repos/internal/docker-compose-collection/tooling/golang-build
GOOS ?= linux
GOARCH ?= amd64

.PHONY: build build-image build-local fmt vet test run update

# growstar-jp/docker-compose-collection の tooling/golang-build コンテナでビルドする。
# ホストにGoツールチェインが無くても、Docker越しに静的バイナリ(bin/zengin-api)を作れる。
# デプロイはこのバイナリをそのまま配布する(Dockerはビルド専用、実行はsystemd)。
build:
	WORKSPACE=$(CURDIR) OUT_DIR=$(CURDIR)/bin \
	docker compose -f $(GOLANG_BUILD_DIR)/compose.yml run --rm builder \
		sh -c 'cd /workspace && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
			go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/$(APP) ./cmd/$(APP)'

# Dockerfile変更後にビルダーイメージを作り直す。
build-image:
	docker compose -f $(GOLANG_BUILD_DIR)/compose.yml build

# ホストにGoが入っている場合の直接ビルド(開発機での動作確認用)。
build-local:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(APP) ./cmd/$(APP)

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

run:
	go run ./cmd/$(APP) serve

update:
	go run ./cmd/$(APP) update
