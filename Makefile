# clisesh — Makefile
# Common developer tasks. Run `make help` for a summary.

BINARY      := clisesh
PKG         := github.com/semigodking/clisesh
CMD         := ./cmd/clisesh
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS     := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build a static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: run
run: ## Run the app
	go run $(CMD)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test-v
test-v: ## Run all tests verbosely
	go test -v ./...

.PHONY: cover
cover: ## Run tests with coverage summary
	go test -cover ./...

GOFMT       := $(shell command -v gofmt 2>/dev/null || echo "$(shell go env GOROOT)/bin/gofmt")

.PHONY: lint
lint: ## Run go vet (and gofmt check)
	go vet ./...
	@test -z "$$($(GOFMT) -l . )" || (echo "gofmt needed on:"; $(GOFMT) -l .; exit 1)

.PHONY: fmt
fmt: ## Format all Go source
	$(GOFMT) -w .

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
