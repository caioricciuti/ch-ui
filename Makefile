# CH-UI Cloud Makefile
# Single binary: server + agent + embedded frontend

VERSION ?= $(shell cat VERSION 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS = -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(DATE)

BINARY = ch-ui

.PHONY: app build rebuild from-scratch build-frontend build-go dev test clean tidy vet help

## app: Build frontend + Go binary (production-ready)
app: build-frontend build-go

## build: Build everything (frontend + Go binary)
build: app

## rebuild: Clean artifacts, then build everything
rebuild:
	$(MAKE) clean
	$(MAKE) build

## from-scratch: Alias for rebuild
from-scratch: rebuild

## build-frontend: Build the React frontend
build-frontend:
	cd ui && bun install
	@cd ui && (CHUI_VITE_MINIFY=true CHUI_VITE_REPORT_COMPRESSED=false bun run build || \
		(echo "Frontend build was killed; retrying with low-memory profile (no minify)..." && \
		CHUI_VITE_MINIFY=false CHUI_VITE_REPORT_COMPRESSED=false bun run build))

## build-go: Build just the Go binary (skip frontend rebuild)
build-go:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## dev: Start the server in dev mode (expects Vite running on :5173)
dev:
	go run -ldflags "$(LDFLAGS)" . server --dev

## test: Run all Go tests
test:
	go test ./... -v -count=1

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf ui/dist/

## tidy: Clean up Go modules
tidy:
	go mod tidy

## vet: Run go vet
vet:
	go vet ./...

## help: Show this help message
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
