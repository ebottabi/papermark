# Papermark Agent Server Makefile

.PHONY: all build build-worker build-fake build-all run run-worker run-fake dev clean test test-race test-coverage lint fmt deps env deploy help

# Default target
all: build

# Build the server binary (HTTP API + in-process job worker)
build:
	@echo "Building papermark server..."
	@go build -o bin/server ./cmd/server
	@echo "Build complete: bin/server"

# Build the worker binary
build-worker:
	@echo "Building papermark worker..."
	@go build -o bin/worker ./cmd/worker
	@echo "Build complete: bin/worker"

# Build the scripted Anthropic stand-in
build-fake:
	@go build -o bin/fakeanthropic ./cmd/fakeanthropic

# Build all binaries
build-all: build build-worker build-fake

# Run the server
run: build
	@./bin/server

# Run a standalone worker
run-worker: build-worker
	@./bin/worker

# Run the fake Messages API on :8790 with the Lisbon script
run-fake: build-fake
	@./bin/fakeanthropic -port 8790 -script testdata/lisbon.json

# Run in development mode with hot reload (requires air)
dev:
	@which air > /dev/null || (echo "Installing air..." && go install github.com/air-verse/air@latest)
	@air

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/ tmp/
	@echo "Clean complete"

# Run tests (needs Postgres and Redis; see README)
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with the race detector
test-race:
	@go test -race ./...

# Run tests with coverage
test-coverage:
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@golangci-lint run

# Format code
fmt:
	@go fmt ./...

# Download and tidy dependencies
deps:
	@go mod download
	@go mod tidy

# Create .env from example
env:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env from .env.example"; else echo ".env already exists"; fi

# Deploy to a bare-metal host (see deploy/)
deploy:
	@echo "Copy bin/server and bin/worker to /opt/papermark/bin and install deploy/*.service (see deploy/)."

# Help
help:
	@echo "Papermark Agent Server - Available commands:"
	@echo "  make build         - Build the server binary"
	@echo "  make build-worker  - Build the worker binary"
	@echo "  make build-fake    - Build the fake Anthropic API"
	@echo "  make build-all     - Build all binaries"
	@echo "  make run           - Build and run the server"
	@echo "  make run-worker    - Build and run a standalone worker"
	@echo "  make run-fake      - Build and run the fake Anthropic API on :8790"
	@echo "  make dev           - Run with hot reload (requires air)"
	@echo "  make clean         - Remove build artifacts"
	@echo "  make test          - Run tests"
	@echo "  make test-race     - Run tests with the race detector"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make lint          - Run linter"
	@echo "  make fmt           - Format code"
	@echo "  make deps          - Download and tidy dependencies"
	@echo "  make env           - Create .env from .env.example"
