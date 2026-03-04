#!/bin/bash


set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------------------
# .env setup
# ---------------------------------------------------------------------------
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

# Google OAuth (optional for local dev)
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret
GOOGLE_REDIRECT_URL=http://localhost:8080/api/v1/auth/google/callback

# Database credential encryption key (used for AES-GCM in utils/crypto.go)
# MUST be the same across restarts for existing credentials to remain usable.
DB_CRED_ENCRYPTION_KEY=change-this-to-a-long-random-secret

# Kubernetes operator provisioner (optional — required for project DB provisioning)
# DB_INSTANCES_NAMESPACE=default
# KUBECONFIG=
EOF

        print_success ".env file created. Please update it with your configuration."
        print_warning "IMPORTANT: Change the JWT secrets before production!"
    else
        print_success ".env file exists"
    fi

    # Export env vars for the current shell
    set -a
    # shellcheck disable=SC1091
    source .env 2>/dev/null || true
    set +a
}

# ---------------------------------------------------------------------------
# Docker / Compose
# ---------------------------------------------------------------------------
check_docker() {
    print_info "Checking Docker..."
    if ! command_exists docker; then
        print_error "Docker is required. Install from https://docs.docker.com/get-docker/"
        exit 1
    fi
    if ! docker info >/dev/null 2>&1; then
        print_error "Docker daemon is not running. Start it and try again."
        exit 1
    fi
    print_success "Docker is installed and running"
}

detect_compose_cmd() {
    if docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    elif command_exists docker-compose; then
        COMPOSE_CMD="docker-compose"
    else
        print_error "Docker Compose is required (docker compose or docker-compose)."
        exit 1
    fi
}

start_containers() {
    print_info "Starting Docker containers (PostgreSQL)..."

    if docker ps --format '{{.Names}}' | grep -q '^dbaas-postgres$'; then
        print_success "PostgreSQL container is already running"
    else
        detect_compose_cmd
        $COMPOSE_CMD up -d || { print_error "Failed to start containers"; exit 1; }
    fi

    # Wait for PostgreSQL to be ready
    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
            print_success "PostgreSQL is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    print_error "PostgreSQL did not become ready within ${max_attempts}s"
    exit 1
}

# ---------------------------------------------------------------------------
# Database creation
# ---------------------------------------------------------------------------
setup_database() {
    print_info "Setting up database..."
    local POSTGRES_SUPERUSER="postgres"
    local DB_NAME="${DB_DATABASE:-dbaas}"

    if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw "$DB_NAME"; then
        print_success "Database '$DB_NAME' already exists"
    else
        print_info "Creating database '$DB_NAME'..."
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "CREATE DATABASE \"$DB_NAME\";" 2>/dev/null || {
            print_error "Failed to create database"
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

# ---------------------------------------------------------------------------
# Go build & run
# ---------------------------------------------------------------------------
check_go() {
    print_info "Checking Go..."
    if ! command_exists go; then
        print_error "Go is required. Install from https://go.dev/dl/"
        exit 1
    fi
    print_success "Go $(go version | awk '{print $3}') available"
}

build_and_run() {
    print_info "Installing dependencies..."
    go mod download

    print_info "Building binary..."
    go build -o bin/api ./cmd/api || { print_error "Build failed"; exit 1; }
    print_success "Binary built: bin/api"

    echo ""
    print_info "Starting server on http://localhost:${PORT:-8080}"
    print_info "Press Ctrl+C to stop"
    echo ""
    exec ./bin/api
}

# ---------------------------------------------------------------------------
# Cleanup on exit
# ---------------------------------------------------------------------------
cleanup() {
    echo ""
    print_info "Shutting down..."
    detect_compose_cmd 2>/dev/null || true
    ${COMPOSE_CMD:-docker compose} stop 2>/dev/null || true
    exit 0
}

trap cleanup SIGINT SIGTERM

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    echo "=========================================="
    echo "  Database-as-a-Service Backend (dev)"
    echo "=========================================="
    echo ""

    cd "$(dirname "$0")"

    check_go
    check_docker
    setup_env
    start_containers
    setup_database
    build_and_run
}

main "$@"
