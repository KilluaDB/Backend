.PHONY: help install deps build run test clean migrate setup docker-down docker-build docker-push deploy

# Default target
.DEFAULT_GOAL := help

# Variables
BINARY_NAME=api
BINARY_PATH=bin/$(BINARY_NAME)
MAIN_PATH=cmd/api/main.go
DOCKERHUB_USERNAME ?= your-dockerhub-username
BACKEND_IMAGE ?= $(DOCKERHUB_USERNAME)/dbaas-backend:latest

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

docker-build: ## Build Docker image (set DOCKERHUB_USERNAME or BACKEND_IMAGE)
	@echo "Building Docker image: $(BACKEND_IMAGE)"
	docker build -t $(BACKEND_IMAGE) .

docker-push: docker-build ## Build and push Docker image to DockerHub
	docker push $(BACKEND_IMAGE)

deploy: ## Deploy all K8s manifests to the current cluster context (set DOCKERHUB_USERNAME or BACKEND_IMAGE)
	@if [ "$(BACKEND_IMAGE)" = "your-dockerhub-username/dbaas-backend:latest" ]; then \
		echo "ERROR: Set DOCKERHUB_USERNAME or BACKEND_IMAGE before deploying"; exit 1; fi
	@echo "Deploying to Kubernetes..."
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/redis.yaml
	kubectl apply -f deploy/meta-db-cluster.yaml
	@echo "Waiting for meta-db to be ready (up to 5 min)..."
	kubectl wait --for=condition=Ready cluster/meta-db --timeout=300s 2>/dev/null || echo "Still waiting for CNPG cluster (check: kubectl get cluster meta-db)"
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/secret.yaml
	BACKEND_IMAGE=$(BACKEND_IMAGE) envsubst < deploy/deployment.yaml | kubectl apply -f -
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
