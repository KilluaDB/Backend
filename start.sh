#!/bin/bash

# Database-as-a-Service Backend Startup Script (k3d)
# Single path: start meta-DB containers, build image, deploy to k3d, port-forward, stream logs.

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if port is in use (used for choosing port-forward port)
port_in_use() {
    local port=$1
    if command_exists netstat; then
        netstat -tuln 2>/dev/null | grep -q ":$port " && return 0
    elif command_exists ss; then
        ss -tuln 2>/dev/null | grep -q ":$port " && return 0
    elif command_exists lsof; then
        lsof -i :$port >/dev/null 2>&1 && return 0
    fi
    return 1
}

# Create .env file if it doesn't exist
setup_env() {
    print_info "Checking environment configuration..."

    if [ ! -f .env ]; then
        print_warning ".env file not found. Creating from template..."

        cat > .env << 'EOF'
# Application Configuration
PORT=8080

# Database Configuration (meta-DB for the backend)
DB_HOST=localhost
DB_PORT=5432
DB_USERNAME=postgres
DB_PASSWORD=postgres
DB_DATABASE=dbaas
DB_ADMIN_USER=postgres
DB_ADMIN_PASSWORD=postgres

# JWT Secrets (CHANGE THESE IN PRODUCTION!)
ACCESS_TOKEN_SECRET=your-access-token-secret-change-this-in-production
REFRESH_TOKEN_SECRET=your-refresh-token-secret-change-this-in-production

# Google OAuth (optional)
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret
GOOGLE_REDIRECT_URL=http://localhost:8080/api/v1/auth/google/callback

# Database credential encryption key (used for AES-GCM in utils/crypto.go)
# MUST be the same across restarts for existing credentials to remain usable.
DB_CRED_ENCRYPTION_KEY=change-this-to-a-long-random-secret

# Kubernetes operator provisioner (optional)
# DB_INSTANCES_NAMESPACE=default
# KUBECONFIG=
EOF

        print_success ".env file created. Please update it with your configuration."
        print_warning "IMPORTANT: Change the JWT secrets in production!"
    else
        print_success ".env file exists"
    fi

    export $(grep -v '^#' .env | xargs 2>/dev/null) || true
}

# --- Docker check (for building image and meta-DB containers) ---
check_docker() {
    print_info "Checking Docker installation..."
    if ! command_exists docker; then
        print_error "Docker is required. Install from https://docs.docker.com/get-docker/"
        exit 1
    fi
    if ! docker info >/dev/null 2>&1; then
        print_error "Docker daemon is not running. Start it (e.g. sudo systemctl start docker) and try again."
        exit 1
    fi
    print_success "Docker is installed and running"
}

# --- Meta-DB containers and DB setup (backend in k8s connects to Postgres on host) ---
stop_conflicting_services() {
    if ! port_in_use 5432 && ! port_in_use 6379; then
        return 0
    fi
    print_info "Checking for conflicting services on ports 5432 and 6379..."
    if port_in_use 5432; then
        print_warning "Port 5432 is in use. Ensure PostgreSQL is not conflicting with our container."
    fi
    if port_in_use 6379; then
        print_warning "Port 6379 is in use. Our Redis container may fail to start."
    fi
}

start_containers() {
    print_info "Starting Docker containers (PostgreSQL and Redis)..."
    stop_conflicting_services

    if command_exists docker-compose; then
        COMPOSE_CMD="docker-compose"
    elif docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    else
        print_error "Docker Compose is required (docker-compose or docker compose)."
        exit 1
    fi

    if docker ps --format '{{.Names}}' | grep -q '^dbaas-postgres$' && \
       docker ps --format '{{.Names}}' | grep -q '^dbaas-redis$'; then
        print_success "Containers are already running"
        return 0
    fi

    if [ ! -f docker-compose.yml ]; then
        print_error "docker-compose.yml not found"
        exit 1
    fi
    $COMPOSE_CMD up -d || { print_error "Failed to start containers"; exit 1; }

    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
            print_success "PostgreSQL is ready"
            break
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    if [ $attempt -eq $max_attempts ]; then
        print_error "PostgreSQL did not become ready"
        exit 1
    fi

    attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec dbaas-redis redis-cli ping >/dev/null 2>&1; then
            print_success "Redis is ready"
            break
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    if [ $attempt -eq $max_attempts ]; then
        print_warning "Redis did not become ready; continuing anyway"
    fi
}

check_postgresql() {
    if ! docker ps --format '{{.Names}}' | grep -q '^dbaas-postgres$'; then
        print_warning "PostgreSQL container not running. Starting containers..."
        start_containers
        return
    fi
    if ! docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
        print_warning "PostgreSQL not ready. Waiting..."
        sleep 3
    fi
    print_success "PostgreSQL container is running"
}

check_redis() {
    if ! docker ps --format '{{.Names}}' | grep -q '^dbaas-redis$'; then
        print_warning "Redis container not running. Starting containers..."
        start_containers
        return
    fi
    print_success "Redis container is running"
}

setup_database() {
    print_info "Setting up database..."
    setup_env
    local POSTGRES_SUPERUSER="postgres"
    local DB_NAME="${DB_DATABASE:-dbaas}"

    if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw "$DB_NAME"; then
        print_success "Database '$DB_NAME' already exists"
    else
        print_info "Creating database '$DB_NAME'..."
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "CREATE DATABASE \"$DB_NAME\";" 2>/dev/null || {
            print_error "Failed to create database. Try: docker exec -it dbaas-postgres psql -U postgres -c 'CREATE DATABASE $DB_NAME;'"
            exit 1
        }
        print_success "Database '$DB_NAME' created"
    fi

    if [ -n "$DB_USERNAME" ] && [ "$DB_USERNAME" != "$POSTGRES_SUPERUSER" ]; then
        if ! docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -tAc "SELECT 1 FROM pg_roles WHERE rolname='$DB_USERNAME'" 2>/dev/null | grep -q 1; then
            docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "CREATE USER \"$DB_USERNAME\" WITH PASSWORD '${DB_PASSWORD:-postgres}';" 2>/dev/null || true
        fi
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "GRANT ALL PRIVILEGES ON DATABASE \"$DB_NAME\" TO \"$DB_USERNAME\";" 2>/dev/null || true
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -d "$DB_NAME" -c "GRANT ALL ON SCHEMA public TO \"$DB_USERNAME\"; ALTER SCHEMA public OWNER TO \"$DB_USERNAME\";" 2>/dev/null || true
    fi
}

# --- Kubernetes: run backend in cluster and port-forward ---
check_kubectl() {
    print_info "Checking kubectl..."
    if ! command_exists kubectl; then
        print_error "kubectl is not installed. Install from https://kubernetes.io/docs/tasks/tools/"
        exit 1
    fi
    if ! kubectl cluster-info >/dev/null 2>&1; then
        print_error "No Kubernetes cluster reachable. Run ./scripts/k3s-local-setup.sh to create a local k3d cluster."
        exit 1
    fi
    print_success "kubectl is available and cluster is reachable"
}

k8s_meta_db_host() {
    local ctx
    ctx=$(kubectl config current-context 2>/dev/null || true)
    if printf '%s\n' "$ctx" | grep -q '^k3d-'; then
        echo "host.k3d.internal"
    else
        echo "host.docker.internal"
    fi
}

# K8s mode: port-forward PID and cleanup flag
PF_PID=""
K8S_CLEANUP=""

run_in_k8s() {
    print_info "Running backend in Kubernetes cluster (deploy + port-forward)..."
    check_kubectl
    check_docker

    setup_env
    start_containers
    check_postgresql
    check_redis
    setup_database

    export $(grep -v '^#' .env | xargs 2>/dev/null)
    local meta_host
    meta_host=$(k8s_meta_db_host)
    local namespace="${DB_INSTANCES_NAMESPACE:-default}"

    print_info "Building backend Docker image..."
    docker build -t backend-api:latest . || { print_error "Docker build failed"; exit 1; }

    local ctx
    ctx=$(kubectl config current-context 2>/dev/null || true)
    if printf '%s\n' "$ctx" | grep -q '^k3d-'; then
        if command_exists k3d; then
            local cluster
            cluster=${ctx#k3d-}
            if [ -n "$cluster" ]; then
                print_info "Importing image into k3d cluster '$cluster'..."
                k3d image import backend-api:latest -c "$cluster" 2>/dev/null || true
            fi
        fi
    fi
    print_success "Image built"

    print_info "Updating ConfigMap (DB_HOST=$meta_host, namespace=$namespace)..."
    kubectl create configmap backend-config \
        --from-literal=PORT=8080 \
        --from-literal=DB_HOST="$meta_host" \
        --from-literal=DB_PORT="${DB_PORT:-5432}" \
        --from-literal=DB_DATABASE="${DB_DATABASE:-dbaas}" \
        --from-literal=DB_USERNAME="${DB_USERNAME:-postgres}" \
        --from-literal=DB_ADMIN_USER="${DB_ADMIN_USER:-postgres}" \
        --from-literal=DB_INSTANCES_NAMESPACE="$namespace" \
        --from-literal=GOOGLE_REDIRECT_URL="${GOOGLE_REDIRECT_URL:-http://localhost:8080/api/v1/auth/google/callback}" \
        -n default --dry-run=client -o yaml | kubectl apply -f - || { print_error "ConfigMap apply failed"; exit 1; }

    print_info "Creating/updating Secret from .env..."
    kubectl create secret generic backend-secrets --from-env-file=.env -n default --dry-run=client -o yaml | kubectl apply -f - || { print_error "Secret apply failed"; exit 1; }

    print_info "Applying deploy manifests..."
    if [ ! -d deploy ]; then
        print_error "deploy/ directory not found"
        exit 1
    fi
    kubectl apply -f deploy/rbac.yaml -f deploy/deployment.yaml -f deploy/service.yaml || { print_error "kubectl apply failed"; exit 1; }

    # Restore replicas if cleanup had scaled to 0; then single rollout restart to pick up new image.
    current_replicas=$(kubectl get deployment backend -n default -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
    if [ "$current_replicas" = "0" ]; then
        kubectl scale deployment backend --replicas=1 -n default || true
    fi
    print_info "Restarting deployment to use new image (one pod only)..."
    kubectl rollout restart deployment/backend -n default || true
    print_info "Waiting for backend pod to be ready..."
    kubectl rollout status deployment/backend -n default --timeout=120s || { print_error "Rollout did not complete"; exit 1; }
    kubectl wait --for=condition=ready pod -l app=backend -n default --timeout=60s 2>/dev/null || true

    print_success "Backend is running in the cluster (single pod)."
    K8S_CLEANUP=1
    k8s_port="${K8S_PORT:-8081}"

    if port_in_use "$k8s_port"; then
        print_warning "Port $k8s_port is already in use"
        pf_pid=$(ss -tlnp 2>/dev/null | grep -E "[:\\[]${k8s_port}[\\]]?\\b" | sed -n 's/.*pid=\([0-9]\+\).*/\1/p' | head -n 1)
        if [ -n "$pf_pid" ]; then
            pf_cmd=$(ps -p "$pf_pid" -o comm= 2>/dev/null || true)
            if [ "$pf_cmd" = "kubectl" ]; then
                print_info "Stopping existing kubectl listener on port $k8s_port (pid $pf_pid)"
                kill "$pf_pid" 2>/dev/null || true
                sleep 1
            fi
        fi
        if port_in_use "$k8s_port"; then
            base_port=$k8s_port
            for p in $(seq $((base_port + 1)) $((base_port + 25))); do
                if ! port_in_use "$p"; then
                    k8s_port=$p
                    print_info "Using free port $k8s_port instead"
                    break
                fi
            done
        fi
    fi

    print_info "Starting port-forward to http://localhost:$k8s_port"
    kubectl port-forward "svc/backend" "${k8s_port}:8080" -n default &
    PF_PID=$!
    sleep 1
    if ! kill -0 "$PF_PID" 2>/dev/null; then
        print_error "Port-forward failed to start"
        K8S_CLEANUP=""
        exit 1
    fi
    echo ""
    print_info "Streaming backend logs (Ctrl+C to stop and scale down backend)"
    echo ""
    kubectl logs -f deploy/backend -n default --all-containers --prefix
}

stop_containers() {
    if command_exists docker-compose; then
        docker-compose stop 2>/dev/null || true
    elif docker compose version >/dev/null 2>&1; then
        docker compose stop 2>/dev/null || true
    else
        docker stop dbaas-postgres dbaas-redis 2>/dev/null || true
    fi
}

cleanup() {
    echo ""
    print_info "Shutting down..."
    if [ -n "$K8S_CLEANUP" ]; then
        if [ -n "$PF_PID" ] && kill -0 "$PF_PID" 2>/dev/null; then
            kill "$PF_PID" 2>/dev/null || true
            print_info "Stopped port-forward (PID $PF_PID)"
        fi
        print_info "Scaling backend deployment to 0..."
        kubectl scale deployment backend --replicas=0 -n default 2>/dev/null || true
        K8S_CLEANUP=""
        PF_PID=""
    fi
    exit 0
}

trap cleanup SIGINT SIGTERM EXIT

main() {
    echo "=========================================="
    echo "  Database-as-a-Service Backend (k3d)"
    echo "=========================================="
    echo ""

    cd "$(dirname "$0")"

    check_docker
    setup_env
    start_containers
    check_postgresql
    check_redis
    setup_database
    run_in_k8s
}

main "$@"
