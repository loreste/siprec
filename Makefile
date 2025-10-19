# Makefile for SIPREC Server
.PHONY: help install deps fmt vet lint check \
	build build-mysql build-test build-all build-race cross-build \
	test test-mysql test-integration test-e2e test-all test-unit test-verbose \
	coverage coverage-mysql benchmark \
	docker-build docker-build-dev docker-test docker-push \
	docker dev-up dev-down dev-logs dev-shell clean docs

# Configuration
PROJECT_NAME := siprec-server
BINARY_NAME := siprec
BINARY_PATH := ./cmd/siprec
TEST_BINARY := testenv
TEST_PATH := ./cmd/testenv

# Build configuration
BUILD_DIR := ./build
DIST_DIR := ./dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go configuration
GO := go
GOCMD := $(GO)
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOVET := $(GOCMD) vet

# Build flags
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"
BUILD_FLAGS := -trimpath
GO_BUILD_TAGS ?=
ifneq ($(strip $(GO_BUILD_TAGS)),)
GO_TAG_ARGS := -tags "$(GO_BUILD_TAGS)"
else
GO_TAG_ARGS :=
endif

# Test configuration
TEST_FLAGS := -race -timeout=5m
TEST_PACKAGES := ./pkg/... ./cmd/... ./test/...
COVERAGE_FILE := coverage.out
COVERAGE_HTML := coverage.html

# Docker configuration
DOCKER_IMAGE := $(PROJECT_NAME)
DOCKER_TAG ?= latest
DOCKER_REGISTRY ?= localhost:5000

# Development configuration
DEV_COMPOSE_FILE := docker-compose.dev.yml
PROD_COMPOSE_FILE := docker-compose.yml

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

install: ## Install development dependencies
	@echo "Installing development dependencies..."
	$(GOGET) -u golang.org/x/tools/cmd/goimports
	$(GOGET) -u github.com/golangci/golangci-lint/cmd/golangci-lint
	$(GOGET) -u gotest.tools/gotestsum
	$(GOGET) -u github.com/swaggo/swag/cmd/swag
	@echo "Development dependencies installed"

deps: ## Download and verify dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) verify
	$(GOMOD) tidy
	@echo "Dependencies updated"

fmt: ## Format Go code
	@echo "Formatting Go code..."
	$(GOFMT) -s -w .
	$(GO) run golang.org/x/tools/cmd/goimports -w .
	@echo "Code formatted"

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOVET) ./...
	@echo "Go vet completed"

lint: ## Run golangci-lint
	@echo "Running golangci-lint..."
	golangci-lint run ./...
	@echo "Linting completed"

check: fmt vet lint ## Run all code quality checks

##@ Build

build: ## Build the main binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(BINARY_PATH)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)"

build-mysql: ## Build the main binary with MySQL support enabled
	@echo "Building $(BINARY_NAME) with MySQL support..."
	$(MAKE) GO_BUILD_TAGS="mysql" build

build-test: ## Build the test environment binary
	@echo "Building $(TEST_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(TEST_BINARY) $(TEST_PATH)
	@echo "Built $(BUILD_DIR)/$(TEST_BINARY)"

build-all: build build-test ## Build all binaries

build-race: ## Build with race detection
	@echo "Building $(BINARY_NAME) with race detection..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) -race $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-race $(BINARY_PATH)
	@echo "Built $(BUILD_DIR)/$(BINARY_NAME)-race"

cross-build: ## Build for multiple platforms
	@echo "Cross-building for multiple platforms..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(BINARY_PATH)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(BINARY_PATH)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 $(BINARY_PATH)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(BINARY_PATH)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(GO_TAG_ARGS) $(BUILD_FLAGS) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(BINARY_PATH)
	@echo "Cross-build completed"

##@ Testing

test: ## Run unit tests
	@echo "Running unit tests..."
	$(GOTEST) $(GO_TAG_ARGS) $(TEST_FLAGS) -short ./pkg/... ./cmd/...

test-mysql: ## Run unit tests with MySQL support enabled
	@echo "Running unit tests with MySQL support..."
	$(MAKE) GO_BUILD_TAGS="mysql" test

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	$(GOTEST) $(GO_TAG_ARGS) $(TEST_FLAGS) -tags=integration ./test/integration/...

test-e2e: ## Run end-to-end tests
	@echo "Running E2E tests..."
	$(GOTEST) $(GO_TAG_ARGS) $(TEST_FLAGS) -tags=e2e ./test/e2e/...

test-all: ## Run all tests
	@echo "Running all tests..."
	./scripts/run-tests.sh --all

test-unit: ## Run unit tests only
	@echo "Running unit tests only..."
	$(GOTEST) $(GO_TAG_ARGS) $(TEST_FLAGS) -short ./pkg/... ./cmd/...

test-verbose: ## Run tests with verbose output
	@echo "Running tests with verbose output..."
	./scripts/run-tests.sh --all --verbose

coverage: ## Generate test coverage report
	@echo "Generating coverage report..."
	$(GOTEST) $(GO_TAG_ARGS) $(TEST_FLAGS) -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./pkg/... ./cmd/...
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	$(GO) tool cover -func=$(COVERAGE_FILE)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

coverage-mysql: ## Generate coverage report with MySQL support enabled
	@echo "Generating coverage report with MySQL support..."
	$(MAKE) GO_BUILD_TAGS="mysql" coverage

test-coverage: coverage ## Alias for coverage
	@echo "Coverage summary:"
	@$(GO) tool cover -func=$(COVERAGE_FILE) | tail -1

benchmark: ## Run benchmark tests
	@echo "Running benchmark tests..."
	$(GOTEST) $(GO_TAG_ARGS) -bench=. -benchmem ./pkg/... ./test/unit/...

##@ Docker

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -f Dockerfile --target production -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo "Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

docker-build-dev: ## Build development Docker image
	@echo "Building development Docker image..."
	docker build -f Dockerfile --target development -t $(DOCKER_IMAGE):dev .
	@echo "Development Docker image built: $(DOCKER_IMAGE):dev"

docker-test: ## Run tests in Docker
	@echo "Running tests in Docker..."
	docker build -f Dockerfile --target tester -t $(DOCKER_IMAGE):test .
	docker run --rm -v $(PWD)/test-results:/build/test-results $(DOCKER_IMAGE):test
	@echo "Docker tests completed"

docker-push: docker-build ## Push Docker image to registry
	@echo "Pushing Docker image to registry..."
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	@echo "Docker image pushed"

##@ Development Environment

dev-up: ## Start development environment
	@echo "Starting development environment..."
	docker-compose -f $(DEV_COMPOSE_FILE) up -d
	@echo "Development environment started"

dev-down: ## Stop development environment
	@echo "Stopping development environment..."
	docker-compose -f $(DEV_COMPOSE_FILE) down
	@echo "Development environment stopped"

dev-logs: ## Show development environment logs
	docker-compose -f $(DEV_COMPOSE_FILE) logs -f

dev-shell: ## Start development shell
	docker-compose -f $(DEV_COMPOSE_FILE) exec siprec-dev /bin/bash

dev-rebuild: ## Rebuild and restart development environment
	@echo "Rebuilding development environment..."
	docker-compose -f $(DEV_COMPOSE_FILE) down
	docker-compose -f $(DEV_COMPOSE_FILE) build --no-cache
	docker-compose -f $(DEV_COMPOSE_FILE) up -d
	@echo "Development environment rebuilt"

##@ Production

prod-up: ## Start production environment
	@echo "Starting production environment..."
	docker-compose -f $(PROD_COMPOSE_FILE) up -d
	@echo "Production environment started"

prod-down: ## Stop production environment
	@echo "Stopping production environment..."
	docker-compose -f $(PROD_COMPOSE_FILE) down
	@echo "Production environment stopped"

prod-logs: ## Show production environment logs
	docker-compose -f $(PROD_COMPOSE_FILE) logs -f

prod-deploy: docker-build docker-push prod-down prod-up ## Deploy to production

##@ Monitoring

logs: ## Show application logs
	@if [ -f ./logs/siprec.log ]; then tail -f ./logs/siprec.log; else echo "No log file found"; fi

metrics: ## Show metrics endpoint
	@curl -s http://localhost:8080/metrics || echo "Metrics endpoint not available"

health: ## Check application health
	@curl -s http://localhost:8080/health || echo "Health endpoint not available"

##@ Documentation

docs: ## Generate documentation
	@echo "Generating documentation..."
	@mkdir -p docs/api
	swag init -g cmd/siprec/main.go -o docs/api
	@echo "Documentation generated in docs/"

docs-serve: ## Serve documentation locally
	@echo "Serving documentation at http://localhost:6060"
	godoc -http=:6060

##@ Utilities

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -rf $(DIST_DIR)
	rm -rf test-results
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	docker system prune -f --volumes || true
	@echo "Clean completed"

clean-all: clean ## Clean everything including Docker images
	@echo "Cleaning Docker images..."
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):dev $(DOCKER_IMAGE):test 2>/dev/null || true
	@echo "Clean all completed"

version: ## Show version information
	@echo "Project: $(PROJECT_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

run: build ## Build and run the application
	@echo "Running $(BINARY_NAME)..."
	$(BUILD_DIR)/$(BINARY_NAME)

run-dev: ## Run in development mode
	@echo "Running in development mode..."
	$(GO) run $(BINARY_PATH) -env=development

debug: build-race ## Build with race detection and run
	@echo "Running $(BINARY_NAME) with race detection..."
	$(BUILD_DIR)/$(BINARY_NAME)-race

##@ Git Hooks

install-hooks: ## Install git hooks
	@echo "Installing git hooks..."
	@mkdir -p .git/hooks
	@echo '#!/bin/bash\nmake check' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo '#!/bin/bash\nmake test-unit' > .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Git hooks installed"

##@ Release

release-prep: clean check test-all cross-build ## Prepare for release
	@echo "Release preparation completed"
	@echo "Binaries available in $(DIST_DIR)/"

release-notes: ## Generate release notes
	@echo "Generating release notes..."
	@echo "# Release $(VERSION)" > RELEASE_NOTES.md
	@echo "" >> RELEASE_NOTES.md
	@echo "## Changes" >> RELEASE_NOTES.md
	@git log --oneline --no-merges $$(git describe --tags --abbrev=0)..HEAD >> RELEASE_NOTES.md
	@echo "Release notes generated in RELEASE_NOTES.md"

##@ Database (if using PostgreSQL)

db-up: ## Start PostgreSQL database
	docker-compose -f $(DEV_COMPOSE_FILE) --profile postgres up -d postgres

db-down: ## Stop PostgreSQL database
	docker-compose -f $(DEV_COMPOSE_FILE) --profile postgres down

db-migrate: ## Run database migrations (placeholder)
	@echo "Database migrations not implemented yet"

db-seed: ## Seed database with test data (placeholder)
	@echo "Database seeding not implemented yet"

##@ Security

security-scan: ## Run security scans
	@echo "Running security scans..."
	govulncheck ./...
	@echo "Security scan completed"

##@ Default

.DEFAULT_GOAL := help
