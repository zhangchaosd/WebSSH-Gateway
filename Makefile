GO ?= go

.PHONY: build test run frontend

frontend:
	npm ci
	npm run build

build: frontend
	$(GO) build -trimpath -ldflags "-s -w" -o webssh ./cmd/webssh

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/webssh
