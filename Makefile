.DEFAULT_GOAL := build
SHELL := /bin/bash

GO        ?= go
NPM       ?= npm
WEB_DIR   := web
BIN_DIR   := bin
BIN_NAME  := flight-tracker

.PHONY: build build-go build-web run dev test test-go lint lint-go lint-web \
        typecheck-web fmt-web clean tidy

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

test: test-go

test-go:
	$(GO) test ./...

lint: lint-go lint-web

lint-go:
	$(GO) vet ./...

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
