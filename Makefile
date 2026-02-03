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

# Run in production mode (sets ENVIRONMENT=production)
run-prod: build
	@ENVIRONMENT=production ./scripts/start.sh

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

# Production Docker commands
docker-prod-up:
	@if [ ! -f .env.production ]; then \
		echo "❌ Error: .env.production not found"; \
		echo "   Copy .env.production.template to .env.production and configure it"; \
		exit 1; \
	fi
	@docker-compose -f docker-compose.prod.yml --env-file .env.production up -d
	@echo "✓ Production Docker services started"

docker-prod-down:
	@docker-compose -f docker-compose.prod.yml down
	@echo "✓ Production Docker services stopped"

docker-prod-logs:
	@docker-compose -f docker-compose.prod.yml logs -f

docker-prod-build:
	@docker-compose -f docker-compose.prod.yml build

doRun Docker in production mode (sets ENVIRONMENT=production)
docker-prod:
	@ENVIRONMENT=production docker-compose up -d
	@echo "✓ Docker services started in production mode

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
	@echo "    make build          - Build all services"
	@echo "    make run            - Build and start in development mode"
	@echo "    make run-prod       - Build and start in production mode"
	@echo "    make stop           - Stop all services"
	@echo "    make status         - Check service status"
	@echo "    make dev            - Run with hot-reload (requires air)"
	@echo ""
	@echo "  Environment:"
	@echo "    make validate-env   - Validate current environment config (ENV=development)"
	@echo "    make validate-prod  - Validate production configuration"
	@echo ""
	@echo "  Testing:"
	@echo "    make test           - Run tests"
	@echo "    make test-cover     - Run tests with coverage"
	@echo "    make demo           - Run interactive demo"
	@echo ""
	@echo "  Docker (Development):"
	@echo "    make docker-up      - Start with docker-compose"
	@echo "    make docker-down    - Stop docker-compose"
	@echo "    make docker-logs    - View docker logs"
	@echo "    make docker-build   - Build docker images"
	@echo ""
	@echo "  Docker (Production):"
	@echo "    make docker-prod-up       - Start production services"
	@echo "    make docker-prod-down     - Stop production services"
	@echo "    make docker-prod-logs     - View production logs"
	@echo "    make docker-prod-build    - Build production images"
	@echo "    make docker-prod-validate - Validate production compose config"
	@echo ""
	@echo "  Redis:"
	@echo "    make redis-start    - Start Redis container"
	@echo "    make redis-stop     - Stop Redis container"
	@echo "    make redis-cli      - Open Redis CLI"
	@echo ""
	@echo "  Misc:"
	@echo "    make clean          - Clean build artifacts"
	@echo "    make deps           - Download dependencies"
	@echo "    make fmt            - Format code"
	@echo "    make lint           - Lint code"
	@echo "    make token          - Generate test JWT token"
	@echo ""
	@echo "  Environment Variables:"
	@echo "    ENV=development|production  - Set environment (default: development)"

 (ENVIRONMENT=production)"
	@echo "    make stop           - Stop all services"
	@echo "    make status         - Check service status"
	@echo "    make dev            - Run with hot-reload (requires air)"
	@echo ""
	@echo "  Testing:"
	@echo "    make test           - Run tests"
	@echo "    make test-cover     - Run tests with coverage"
	@echo "    make demo           - Run interactive demo"
	@echo ""
	@echo "  Docker:"
	@echo "    make docker-up      - Start with docker-compose"
	@echo "    make docker-down    - Stop docker-compose"
	@echo "    make docker-logs    - View docker logs"
	@echo "    make docker-build   - Build docker images"
	@echo "    make docker-prod    - Start in production mode (ENVIRONMENT=production)"
	@echo ""
	@echo "  Redis:"
	@echo "    make redis-start    - Start Redis container"
	@echo "    make redis-stop     - Stop Redis container"
	@echo "    make redis-cli      - Open Redis CLI"
	@echo ""
	@echo "  Misc:"
	@echo "    make clean          - Clean build artifacts"
	@echo "    make deps           - Download dependencies"
	@echo "    make fmt            - Format code"
	@echo "    make lint           - Lint code"
	@echo "    make token          - Generate test JWT token"
	@echo ""
	@echo "  Environment:"
	@echo "    Set ENVIRONMENT=development|production to control runtime mode"
	@echo "    Config is validated at startup based on ENVIRONMENT value