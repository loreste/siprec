.PHONY: build run test clean fmt vet lint docker docker-run env-test check-env

APP_NAME=siprec-server
GO_FILES=$(shell find . -name "*.go" -type f -not -path "./vendor/*")
GO_PACKAGES=$(shell go list ./...)
MAIN_PACKAGE=./cmd/siprec

# Default target
all: build

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	@go build -o $(APP_NAME) $(MAIN_PACKAGE)

# Run the application
run:
	@echo "Running $(APP_NAME)..."
	@go run $(MAIN_PACKAGE)

# Run the environment test
env-test:
	@echo "Testing environment..."
	@go run ./cmd/testenv

# Run tests
test:
	@echo "Running tests..."
	@go test -v $(GO_PACKAGES)

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out $(GO_PACKAGES)
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

# Run specific package tests
test-package:
	@echo "Usage: make test-package PKG=./pkg/stt"
	@if [ "$(PKG)" != "" ]; then \
		echo "Running tests for $(PKG)..."; \
		go test -v $(PKG); \
	fi

# Run end-to-end tests
test-e2e:
	@echo "Running end-to-end tests..."
	@go test -v ./test/e2e

# Run specific end-to-end test
test-e2e-case:
	@echo "Usage: make test-e2e-case TEST=TestSiprecCallFlow"
	@if [ "$(TEST)" != "" ]; then \
		echo "Running end-to-end test $(TEST)..."; \
		go test -v ./test/e2e -run $(TEST); \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(APP_NAME)
	@rm -rf ./recordings/*.wav
	@rm -f coverage.out coverage.html

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt $(GO_PACKAGES)

# Run go vet
vet:
	@echo "Vetting code..."
	@go vet $(GO_PACKAGES)

# Run golint if available
lint:
	@if command -v golint >/dev/null 2>&1; then \
		echo "Linting code..."; \
		golint -set_exit_status $(GO_PACKAGES); \
	else \
		echo "golint not installed. Installing..."; \
		go install golang.org/x/lint/golint@latest; \
		echo "Linting code..."; \
		golint -set_exit_status $(GO_PACKAGES); \
	fi

# Check for required environment variables
check-env:
	@echo "Checking for required environment variables..."
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found"; \
		echo "Please copy .env.example to .env and configure it"; \
		exit 1; \
	fi
	@echo "Environment configuration OK"

# Ensure recordings directory exists
ensure-dirs:
	@echo "Ensuring required directories exist..."
	@mkdir -p recordings
	@mkdir -p certs
	@echo "Directories OK"

# Build Docker image
docker:
	@echo "Building Docker image..."
	@docker build -t $(APP_NAME) .

# Run Docker container
docker-run:
	@echo "Running Docker container..."
	@docker run -p 5060:5060/udp -p 5060:5060/tcp -p 8080:8080 --name $(APP_NAME) $(APP_NAME)

# Start all services with docker-compose
docker-up:
	@echo "Starting all services with docker-compose..."
	@docker-compose up -d

# Stop all services with docker-compose
docker-down:
	@echo "Stopping all services with docker-compose..."
	@docker-compose down

# View logs from docker-compose services
docker-logs:
	@echo "Viewing logs from all services..."
	@docker-compose logs -f

# Complete setup (build everything and check environment)
setup: check-env ensure-dirs build

# Help
help:
	@echo "Available targets:"
	@echo "  build          - Build the application"
	@echo "  run            - Run the application locally"
	@echo "  env-test       - Test the environment configuration"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  test-package   - Run tests for a specific package (e.g., make test-package PKG=./pkg/stt)"
	@echo "  test-e2e       - Run end-to-end tests"
	@echo "  test-e2e-case  - Run a specific end-to-end test (e.g., make test-e2e-case TEST=TestSiprecCallFlow)"
	@echo "  clean          - Remove build artifacts"
	@echo "  fmt            - Format code with go fmt"
	@echo "  vet            - Run go vet"
	@echo "  lint           - Run golint"
	@echo "  check-env      - Check for required environment variables"
	@echo "  ensure-dirs    - Ensure required directories exist"
	@echo "  docker         - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  docker-up      - Start all services with docker-compose"
	@echo "  docker-down    - Stop all services with docker-compose"
	@echo "  docker-logs    - View logs from docker-compose services"
	@echo "  setup          - Complete setup (environment check and build)"