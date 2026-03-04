.PHONY: help install deps build run test clean migrate setup cleanup-network docker-down k8s-local

# Default target
.DEFAULT_GOAL := help

# Variables
BINARY_NAME=api
BINARY_PATH=bin/$(BINARY_NAME)
MAIN_PATH=cmd/api/main.go

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install all dependencies (Go, PostgreSQL, Redis)
	@echo "Checking dependencies..."
	@./start.sh --check-only || echo "Please install missing dependencies manually"

deps: ## Install Go dependencies
	@echo "Installing Go dependencies..."
	@go mod download
	@go mod tidy

build: deps ## Build the application
	@echo "Building application..."
	@mkdir -p bin
	@go build -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "Build complete: $(BINARY_PATH)"

run: ## Run the application (development mode)
	@echo "Starting application..."
	@go run $(MAIN_PATH)

start: ## Start backend in k3d (meta-DB, build image, deploy, port-forward, logs)
	@./start.sh

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean

migrate: ## Run database migrations manually
	@echo "Running migrations..."
	@go run $(MAIN_PATH) --migrate-only || echo "Migrations run automatically on startup"

setup: ## Initial setup (create .env, setup database)
	@echo "Setting up project..."
	@if [ ! -f .env ]; then \
		echo "Creating .env file..."; \
		./start.sh --setup-only; \
	fi
	@echo "Setup complete. Please review .env file and update as needed."

check: ## Check if all dependencies are installed
	@echo "Checking dependencies..."
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed"; exit 1; }
	@command -v psql >/dev/null 2>&1 || { echo "PostgreSQL is not installed"; exit 1; }
	@command -v redis-cli >/dev/null 2>&1 || { echo "Redis is not installed (optional)"; }
	@echo "All required dependencies are installed"

k8s-local: ## Start local K3s cluster (k3d) and install CloudNativePG + MongoDB operators
	@./scripts/k3s-local-setup.sh

dev: deps run ## Install dependencies and run in development mode

docker-down: ## Stop and remove Docker containers
	@echo "Stopping Docker containers..."
	@if command -v docker-compose >/dev/null 2>&1; then \
		docker-compose down; \
	elif docker compose version >/dev/null 2>&1; then \
		docker compose down; \
	else \
		echo "Docker Compose not found"; \
	fi
