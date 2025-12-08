.PHONY: all build clean test fmt vet lint deps dev install help

# Variables
BINARY_NAME=kafkaspectre
BIN_DIR=bin
CMD_PATH=./cmd/kafkaspectre
VERSION?=dev
GIT_COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildDate=$(BUILD_DATE) -s -w"

## all: Run full CI pipeline (clean, deps, fmt, vet, test, build)
all: clean deps fmt vet test build

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	@go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -rf dist/
	@go clean
	@echo "Clean complete"

## test: Run tests with race detection and coverage
test:
	@echo "Running tests..."
	@go test -v -race -cover ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## lint: Run golangci-lint (requires golangci-lint to be installed)
lint:
	@echo "Running golangci-lint..."
	@golangci-lint run

## deps: Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

## dev: Run the application in development mode
dev:
	@go run $(CMD_PATH) $(ARGS)

## install: Install the binary to GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	@go install $(LDFLAGS) $(CMD_PATH)
	@echo "Installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

## run-local: Run against local Kafka (assumes kafka:9092)
run-local:
	@$(BIN_DIR)/$(BINARY_NAME) audit --bootstrap-server localhost:9092

## help: Show this help message
help:
	@echo "KafkaSpectre Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' Makefile
