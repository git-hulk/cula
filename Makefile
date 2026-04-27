BINARY  := cula
PKG     := ./cmd/cula
BIN_DIR := bin
GO      ?= go

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build install run test cover fmt vet tidy clean help

all: fmt vet test build

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

install:
	$(GO) install -ldflags '$(LDFLAGS)' $(PKG)

run:
	$(GO) run $(PKG)

test:
	$(GO) test ./... -race

cover:
	$(GO) test ./... -race -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

clean:
	rm -rf $(BIN_DIR) coverage.out

help:
	@echo "Targets:"
	@echo "  build    Build $(BINARY) into $(BIN_DIR)/"
	@echo "  install  Install $(BINARY) to GOBIN"
	@echo "  run      Run the CLI without installing"
	@echo "  test     Run tests with race detector"
	@echo "  cover    Run tests and print coverage summary"
	@echo "  fmt      Format sources"
	@echo "  vet      Run go vet"
	@echo "  tidy     Tidy go.mod"
	@echo "  clean    Remove build artifacts"
