.PHONY: help install deps build run test clean migrate setup docker-down docker-build docker-push deploy

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

install: ## Install all dependencies (Go, PostgreSQL)
	@echo "Checking dependencies..."
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed"; exit 1; }

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

setup: ## Initial setup (create .env)
	@echo "Setting up project..."
	@if [ ! -f .env ]; then \
		echo "Creating .env file from template..."; \
		cp .env.example .env 2>/dev/null || echo "No .env.example found. Create .env manually."; \
	fi
	@echo "Setup complete. Please review .env file and update as needed."

check: ## Check if all dependencies are installed
	@echo "Checking dependencies..."
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed"; exit 1; }
	@command -v docker >/dev/null 2>&1 || { echo "Docker is not installed"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is not installed"; exit 1; }
	@echo "All required dependencies are installed"

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t backend-api:latest .

docker-push: ## Push Docker image to ECR (requires ECR_URL env var)
	@if [ -z "$$ECR_URL" ]; then echo "ECR_URL is required. Run: export ECR_URL=\$$(cd infra/terraform && terraform output -raw ecr_repository_url)"; exit 1; fi
	docker tag backend-api:latest $$ECR_URL:latest
	docker push $$ECR_URL:latest

deploy: ## Deploy all K8s manifests to the current cluster context
	@echo "Deploying to Kubernetes..."
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/meta-db-cluster.yaml
	@echo "Waiting for meta-db to be ready..."
	kubectl wait --for=condition=Ready cluster/meta-db --timeout=300s 2>/dev/null || echo "Waiting for CNPG cluster (check: kubectl get cluster meta-db)"
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/deployment.yaml
	kubectl apply -f deploy/service.yaml
	kubectl apply -f deploy/ingress.yaml
	@echo "Deploy complete. Check: kubectl get pods -n default"

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
