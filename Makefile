# Find Bugs Web monorepo: apps/server (Go) + apps/web (React).
SERVER := apps/server
WEB    := apps/web
BIN    := bin/findbugs
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build build-web build-server openapi test lint run-dev clean

all: build

## build: React build embedded into a single Linux binary at bin/findbugs
build: build-web build-server

build-web:
	cd $(WEB) && npm ci && npm run build
	rm -rf $(SERVER)/web/dist && cp -r $(WEB)/dist $(SERVER)/web/dist

build-server:
	cd $(SERVER) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embed_web \
		-ldflags "-s -w -X main.version=$(VERSION)" -o ../../$(BIN) ./cmd/server

## openapi: regenerate the spec from the Go routes, then the TS types
openapi:
	cd $(SERVER) && go run ./cmd/server openapi > api/openapi.json
	cd $(WEB) && npm run gen:api

test:
	cd $(SERVER) && go vet ./... && go test ./...
	cd $(WEB) && npm run lint && npx tsc -b

lint:
	cd $(SERVER) && test -z "$$(gofmt -l .)" && go vet ./...
	cd $(WEB) && npm run lint

## run-dev: backend with fake GlobalProtect, fake Splunk and fake analyzer
run-dev:
	@mkdir -p /tmp/fbw-dev
	@printf '#!/bin/sh\nprintf "%%s\\n" "$$1" > /tmp/fbw-dev/login-url\n' > /tmp/fbw-dev/capture.sh && chmod +x /tmp/fbw-dev/capture.sh
	@printf '{"cookies":{"splunkd_8008":"a","session_id_8008":"b","splunkweb_csrf_token_8008":"c","token_key":"d"}}' > /tmp/fbw-dev/splunk.json
	cd $(SERVER) && go build -o /tmp/fbw-dev/fakesplunk ./cmd/fakesplunk && (/tmp/fbw-dev/fakesplunk & echo $$! > /tmp/fbw-dev/fakesplunk.pid) && \
	DB_PATH=/tmp/fbw-dev/dev.db COOKIE_SECURE=false ANALYZER=fake \
	GP_BIN=$$PWD/internal/vpn/testdata/fake-globalprotect.sh GP_PORTAL=vpn.example.com GP_WEB_DIR=/tmp/fbw-dev BROWSER=/tmp/fbw-dev/capture.sh FAKE_STATUS=Connected \
	SPLUNK_URL=http://127.0.0.1:8089 SPLUNK_SSO_DOMAIN=login.example.com SPLUNK_API_SESSION_PATH=/tmp/fbw-dev/splunk.json \
	SPLUNK_SPL_TEMPLATES='{"prod":"index=app {transaction_id}","staging":"index=stg {transaction_id}"}' SPLUNK_LOGIN_CMD=true \
	WEB_DIR=../web/dist go run ./cmd/server; kill $$(cat /tmp/fbw-dev/fakesplunk.pid) 2>/dev/null || true

clean:
	rm -rf bin $(SERVER)/web/dist $(WEB)/dist
