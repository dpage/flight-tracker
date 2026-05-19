.DEFAULT_GOAL := build
SHELL := /bin/bash

GO        ?= go
NPM       ?= npm
WEB_DIR   := web
BIN_DIR   := bin
BIN_NAME  := flight-tracker

.PHONY: build build-go build-web run dev test test-go test-web cover-go \
        cover-web lint lint-go lint-web typecheck-web fmt-web clean tidy

## build: build the SPA, then the Go binary that embeds it.
build: build-web build-go

build-web:
	cd $(WEB_DIR) && $(NPM) install --no-audit --no-fund
	cd $(WEB_DIR) && $(NPM) run build

build-go:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' \
		-o $(BIN_DIR)/$(BIN_NAME) ./cmd/server

## run: run the built binary with environment variables from .env (if present).
run:
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
		$(BIN_DIR)/$(BIN_NAME)

## dev: start the Go API on :8080 and the Vite dev server on :5173.
##      Vite proxies /api, /auth, /healthz to :8080 so frontend can hot-reload.
dev:
	@if [ ! -f .env ]; then echo "Create .env first (copy .env.example)"; exit 1; fi
	@bash -c "set -a; source ./.env; set +a; \
		($(GO) run ./cmd/server &) && \
		cd $(WEB_DIR) && $(NPM) run dev"

## test: run the Go and web test suites.
test: test-go test-web

# ./... would also descend into web/node_modules; list the source roots
# explicitly so a populated node_modules doesn't get compiled/tested.
GO_PKGS := ./cmd/... ./internal/... ./migrations ./web

test-go:
	$(GO) test $(GO_PKGS)

## cover-go: per-package Go coverage summary.
cover-go:
	$(GO) test -covermode=set -coverprofile=coverage.out $(GO_PKGS)
	$(GO) tool cover -func=coverage.out | tail -1

test-web:
	cd $(WEB_DIR) && $(NPM) run test

## cover-web: web coverage with the per-file 90% threshold gate.
cover-web:
	cd $(WEB_DIR) && $(NPM) run test:coverage

lint: lint-go lint-web

lint-go:
	$(GO) vet $(GO_PKGS)

lint-web:
	cd $(WEB_DIR) && $(NPM) run lint

typecheck-web:
	cd $(WEB_DIR) && $(NPM) run typecheck

fmt-web:
	cd $(WEB_DIR) && $(NPM) run format

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist
