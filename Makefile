# Defaults baked into the binary (public values only). They fall back to
# FT_CLIENT_ID / FT_BROKER_URL from the environment or ./.env, e.g.
# `make build CLIENT_ID=... BROKER_URL=...`.
-include .env
CLIENT_ID  ?= $(FT_CLIENT_ID)
BROKER_URL ?= $(FT_BROKER_URL)
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BINDIR     ?= $(HOME)/.local/bin

CONFIG_PKG := github.com/skyreks00/ft/internal/config
LDFLAGS := -s -w \
	-X $(CONFIG_PKG).Version=$(VERSION) \
	-X $(CONFIG_PKG).DefaultClientID=$(CLIENT_ID) \
	-X $(CONFIG_PKG).DefaultBrokerURL=$(BROKER_URL)

.PHONY: build install test vet broker-install broker-dev broker-test broker-deploy snapshot clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o ft ./cmd/ft

install:
	mkdir -p $(BINDIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/ft ./cmd/ft

test: vet broker-test
	go test ./...

vet:
	go vet ./...

broker-install:
	cd broker && npm install

broker-dev:
	cd broker && npx wrangler dev

broker-test:
	cd broker && npm test && npm run typecheck

broker-deploy:
	cd broker && npx wrangler deploy

# Local release build (no tag, no upload), into dist/.
snapshot:
	FT_CLIENT_ID=$(CLIENT_ID) FT_BROKER_URL=$(BROKER_URL) goreleaser release --snapshot --clean

clean:
	rm -rf ft dist
