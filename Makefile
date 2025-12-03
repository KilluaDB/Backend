.PHONY: help install deps build run test clean migrate setup cleanup-network docker-down

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

start: build ## Build and run the application
	@echo "Starting application..."
	@./$(BINARY_PATH)

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

dev: deps run ## Install dependencies and run in development mode

cleanup-network: ## Remove orchestrator Docker network (optional - network is reused by default)
	@echo "Removing orchestrator network..."
	@if [ -f .env ]; then \
		export $$(cat .env | grep -v '^#' | xargs); \
		NETWORK_NAME="$${ORCHESTRATOR_NETWORK_NAME:-dbaas-orchestrator-network}"; \
		if docker network ls --format '{{.Name}}' | grep -q "^$$NETWORK_NAME$$"; then \
			echo "Removing network: $$NETWORK_NAME"; \
			docker network rm $$NETWORK_NAME 2>/dev/null || echo "Network removed or not found"; \
		else \
			echo "Network $$NETWORK_NAME does not exist"; \
		fi \
	else \
		NETWORK_NAME="dbaas-orchestrator-network"; \
		if docker network ls --format '{{.Name}}' | grep -q "^$$NETWORK_NAME$$"; then \
			echo "Removing network: $$NETWORK_NAME"; \
			docker network rm $$NETWORK_NAME 2>/dev/null || echo "Network removed or not found"; \
		else \
			echo "Network $$NETWORK_NAME does not exist"; \
		fi \
	fi

docker-down: ## Stop and remove Docker containers (keeps orchestrator network)
	@echo "Stopping Docker containers..."
	@if command -v docker-compose >/dev/null 2>&1; then \
		docker-compose down; \
	elif docker compose version >/dev/null 2>&1; then \
		docker compose down; \
	else \
		echo "Docker Compose not found"; \
	fi
