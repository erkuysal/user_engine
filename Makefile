# UserEngine Makefile
# Usage: make [target]

.PHONY: all build run stop status clean test docker-up docker-down help

# Variables
BIN_DIR := bin
LOG_DIR := logs
SERVICES := gateway api worker-sweeper worker-debouncer

# Default target
all: build

# Build all services
build:
	@echo "Building services..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/gateway ./cmd/gateway
	@go build -o $(BIN_DIR)/api ./cmd/api
	@go build -o $(BIN_DIR)/sweeper ./cmd/worker-sweeper
	@go build -o $(BIN_DIR)/debouncer ./cmd/worker-debouncer
	@go build -o $(BIN_DIR)/webhook ./cmd/worker-webhook
	@echo "✓ Build complete"

# Run all services (foreground, for development)
run: build
	@./scripts/start.sh

# Run with hot-reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	@echo "Starting in development mode..."
	@air -c .air.toml

# Stop all services
stop:
	@./scripts/stop.sh

# Check status
status:
	@./scripts/status.sh

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run tests with coverage
test-cover:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR) $(LOG_DIR) coverage.out coverage.html
	@echo "✓ Clean complete"

# Docker commands
docker-up:
	@docker-compose up -d
	@echo "✓ Docker services started"

docker-down:
	@docker-compose down
	@echo "✓ Docker services stopped"

docker-logs:
	@docker-compose logs -f

docker-build:
	@docker-compose build

# Redis commands
redis-start:
	@docker run -d --name userengine-redis -p 6379:6379 redis:7-alpine || docker start userengine-redis
	@echo "✓ Redis started"

redis-stop:
	@docker stop userengine-redis
	@echo "✓ Redis stopped"

redis-cli:
	@redis-cli

# Generate JWT token for testing
token:
	@go run test/demo.go 2>/dev/null | grep -A1 "JWT Token" | tail -1

# Run interactive demo
demo:
	@go run test/interactive.go

# Format code
fmt:
	@go fmt ./...

# Lint code (requires golangci-lint)
lint:
	@golangci-lint run

# Download dependencies
deps:
	@go mod download
	@go mod tidy

# Help
help:
	@echo "UserEngine - Available targets:"
	@echo ""
	@echo "  Build & Run:"
	@echo "    make build      - Build all services"
	@echo "    make run        - Build and start all services"
	@echo "    make stop       - Stop all services"
	@echo "    make status     - Check service status"
	@echo "    make dev        - Run with hot-reload (requires air)"
	@echo ""
	@echo "  Testing:"
	@echo "    make test       - Run tests"
	@echo "    make test-cover - Run tests with coverage"
	@echo "    make demo       - Run interactive demo"
	@echo ""
	@echo "  Docker:"
	@echo "    make docker-up    - Start with docker-compose"
	@echo "    make docker-down  - Stop docker-compose"
	@echo "    make docker-logs  - View docker logs"
	@echo ""
	@echo "  Redis:"
	@echo "    make redis-start - Start Redis container"
	@echo "    make redis-stop  - Stop Redis container"
	@echo "    make redis-cli   - Open Redis CLI"
	@echo ""
	@echo "  Misc:"
	@echo "    make clean      - Clean build artifacts"
	@echo "    make deps       - Download dependencies"
	@echo "    make fmt        - Format code"
	@echo "    make lint       - Lint code"
	@echo "    make token      - Generate test JWT token"

