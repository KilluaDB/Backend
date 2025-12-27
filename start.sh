#!/bin/bash

# Database-as-a-Service Backend Startup Script
# This script ensures all dependencies are installed and starts the application
# PostgreSQL and Redis run in Docker containers
# Containers are automatically stopped when the script exits (Ctrl+C)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
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

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if service is running
service_running() {
    if command_exists systemctl; then
        systemctl is-active --quiet "$1" 2>/dev/null
    elif command_exists service; then
        service "$1" status >/dev/null 2>&1
    else
        # Fallback: try to connect
        case "$1" in
            postgresql|postgres)
                pg_isready -h localhost -p 5432 >/dev/null 2>&1
                ;;
            redis)
                redis-cli ping >/dev/null 2>&1
                ;;
            *)
                return 1
                ;;
        esac
    fi
}

# Check Go installation
check_go() {
    print_info "Checking Go installation..."
    if ! command_exists go; then
        print_warning "Go is not installed!"

        if command_exists apt-get; then
            print_info "Attempting to install Go via apt-get (requires sudo)..."
            sudo apt-get update -y && sudo apt-get install -y golang || {
                print_error "Failed to install Go via apt-get. Please install it manually from https://go.dev/dl/"
                exit 1
            }

            if ! command_exists go; then
                print_error "Go installation seems to have failed. Please install it manually from https://go.dev/dl/"
                exit 1
            fi

            print_success "Go installed successfully"
        else
            print_error "Go is not installed and automatic installation is not supported on this system."
            print_info "Please install Go 1.24 or later from https://go.dev/dl/"
            exit 1
        fi
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    print_success "Go is installed: $GO_VERSION"
}

# Check Docker installation
check_docker() {
    print_info "Checking Docker installation..."
    
    if ! command_exists docker; then
        print_warning "Docker is not installed!"

        if command_exists apt-get; then
            print_info "Attempting to install Docker via apt-get (requires sudo)..."
            # Install Docker dependencies
            sudo apt-get update -y
            sudo apt-get install -y ca-certificates curl gnupg lsb-release || {
                print_error "Failed to install Docker dependencies."
                exit 1
            }
            
            # Add Docker's official GPG key
            sudo install -m 0755 -d /etc/apt/keyrings
            curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg || {
                print_error "Failed to add Docker GPG key."
                exit 1
            }
            sudo chmod a+r /etc/apt/keyrings/docker.gpg
            
            # Set up Docker repository
            echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
            
            # Install Docker
            sudo apt-get update -y
            sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin || {
                print_error "Failed to install Docker. Please install it manually from https://docs.docker.com/get-docker/"
                exit 1
            }
            
            # Add current user to docker group (requires logout/login to take effect)
            sudo usermod -aG docker "$USER" 2>/dev/null || true
            
            if ! command_exists docker; then
                print_error "Docker installation seems to have failed. Please install it manually from https://docs.docker.com/get-docker/"
                exit 1
            fi
            
            print_success "Docker installed successfully"
            print_warning "You may need to log out and log back in for Docker group permissions to take effect."
        else
            print_error "Docker is not installed and automatic installation is not supported on this system."
            print_info "Please install Docker from https://docs.docker.com/get-docker/"
            exit 1
        fi
    fi
    
    # Check if Docker daemon is running
    if ! docker info >/dev/null 2>&1; then
        print_warning "Docker daemon is not running. Attempting to start..."
        
        if command_exists systemctl; then
            sudo systemctl start docker || {
                print_error "Failed to start Docker daemon. Please start it manually: sudo systemctl start docker"
                exit 1
            }
        elif command_exists service; then
            sudo service docker start || {
                print_error "Failed to start Docker daemon. Please start it manually: sudo service docker start"
                exit 1
            }
        else
            print_error "Please start Docker daemon manually and run this script again."
            exit 1
        fi
        
        # Wait for Docker to be ready
        sleep 2
        if ! docker info >/dev/null 2>&1; then
            print_error "Docker daemon failed to start. Please check your installation."
            exit 1
        fi
    fi
    
    # Check for docker-compose or docker compose plugin
    if ! command_exists docker-compose && ! docker compose version >/dev/null 2>&1; then
        print_warning "Docker Compose is not available. Using docker-compose.yml with docker run commands..."
    fi
    
    print_success "Docker is installed and running"
}

# Check if port is in use
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

# Stop existing containers or services using the ports
stop_conflicting_services() {
    print_info "Checking for conflicting services on ports 5432 and 6379..."
    
    # Check PostgreSQL port
    if port_in_use 5432; then
        print_warning "Port 5432 is already in use."
        # Check if it's our container
        if docker ps --format '{{.Names}}' | grep -q '^dbaas-postgres$'; then
            print_info "Port 5432 is used by our PostgreSQL container (already running)"
        else
            print_warning "Port 5432 is used by another service. Our container will fail to start."
            print_info "You may need to stop the existing PostgreSQL service:"
            print_info "  sudo systemctl stop postgresql"
            print_info "  OR: docker stop <container-name>"
        fi
    fi
    
    # Check Redis port
    if port_in_use 6379; then
        print_warning "Port 6379 is already in use."
        # Check if it's our container
        if docker ps --format '{{.Names}}' | grep -q '^dbaas-redis$'; then
            print_info "Port 6379 is used by our Redis container (already running)"
        else
            print_warning "Port 6379 is used by another service. Attempting to stop conflicting Redis..."
            
            # Try to stop system Redis service
            if command_exists systemctl; then
                if systemctl is-active --quiet redis 2>/dev/null || systemctl is-active --quiet redis-server 2>/dev/null; then
                    print_info "Stopping system Redis service..."
                    sudo systemctl stop redis 2>/dev/null || sudo systemctl stop redis-server 2>/dev/null || true
                    sleep 2
                fi
            fi
            
            # Check if port is still in use
            if port_in_use 6379; then
                # Check if it's a Docker container
                local redis_container=$(docker ps -a --filter "publish=6379" --format "{{.Names}}" | head -1)
                if [ -n "$redis_container" ]; then
                    print_info "Found Docker container '$redis_container' using port 6379. Stopping it..."
                    docker stop "$redis_container" 2>/dev/null || true
                    docker rm "$redis_container" 2>/dev/null || true
                    sleep 2
                fi
                
                # Check again
                if port_in_use 6379; then
                    print_error "Port 6379 is still in use. Please stop the conflicting service manually:"
                    print_info "  sudo systemctl stop redis"
                    print_info "  OR: sudo systemctl stop redis-server"
                    if command_exists lsof; then
                        print_info "  OR: Find and kill the process:"
                        print_info "    sudo lsof -ti:6379 | xargs sudo kill"
                    fi
                    print_info "  OR: Stop Docker container: docker ps --filter 'publish=6379'"
                    return 1
                else
                    print_success "Conflicting Redis service stopped"
                fi
            else
                print_success "Conflicting Redis service stopped"
            fi
        fi
    fi
    
    return 0
}

# Start Docker containers (PostgreSQL and Redis)
start_containers() {
    print_info "Starting Docker containers (PostgreSQL and Redis)..."
    
    # Check for conflicting services
    if ! stop_conflicting_services; then
        print_error "Cannot start containers due to port conflicts."
        exit 1
    fi
    
    # Check if docker-compose is available
    if command_exists docker-compose; then
        COMPOSE_CMD="docker-compose"
    elif docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    else
        print_error "Docker Compose is not available. Please install docker-compose."
        exit 1
    fi
    
    # Check if containers are already running
    if docker ps --format '{{.Names}}' | grep -q '^dbaas-postgres$' && \
       docker ps --format '{{.Names}}' | grep -q '^dbaas-redis$'; then
        print_success "Containers are already running"
        return 0
    fi
    
    # Start containers
    if [ -f docker-compose.yml ]; then
        $COMPOSE_CMD up -d || {
            print_error "Failed to start containers. Please check docker-compose.yml and Docker logs."
            print_info "You can check logs with: docker-compose logs"
            exit 1
        }
        
        # Wait for containers to be healthy
        print_info "Waiting for containers to be ready..."
        local max_attempts=30
        local attempt=0
        
        # Wait for PostgreSQL
        while [ $attempt -lt $max_attempts ]; do
            if docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
                print_success "PostgreSQL container is ready"
                break
            fi
            attempt=$((attempt + 1))
            sleep 1
        done
        
        if [ $attempt -eq $max_attempts ]; then
            print_error "PostgreSQL container failed to become ready"
            exit 1
        fi
        
        # Wait for Redis
        attempt=0
        while [ $attempt -lt $max_attempts ]; do
            if docker exec dbaas-redis redis-cli ping >/dev/null 2>&1; then
                print_success "Redis container is ready"
                break
            fi
            attempt=$((attempt + 1))
            sleep 1
        done
        
        if [ $attempt -eq $max_attempts ]; then
            print_warning "Redis container failed to become ready, but continuing..."
        fi
    else
        print_error "docker-compose.yml not found!"
        exit 1
    fi
}

# Check PostgreSQL container
check_postgresql() {
    print_info "Checking PostgreSQL container..."
    
    # Check if PostgreSQL client is available (for database operations)
    if ! command_exists psql; then
        print_warning "PostgreSQL client (psql) is not installed locally."
        
        if command_exists apt-get; then
            print_info "Installing PostgreSQL client utilities (required for database operations)..."
            sudo apt-get update -y && sudo apt-get install -y postgresql-client || {
                print_error "Failed to install PostgreSQL client. Database operations may fail."
                print_info "You can still use Docker exec: docker exec -it dbaas-postgres psql -U postgres"
            }
        else
            print_warning "PostgreSQL client not available. Database operations will use Docker exec."
        fi
    fi
    
    # Check if container is running
    if ! docker ps | grep -q dbaas-postgres; then
        print_warning "PostgreSQL container is not running. Starting it..."
        start_containers
        return
    fi
    
    # Check if PostgreSQL is ready
    if ! docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
        print_warning "PostgreSQL container is not ready. Waiting..."
        sleep 3
        if ! docker exec dbaas-postgres pg_isready -U postgres >/dev/null 2>&1; then
            print_error "PostgreSQL container is not responding"
            exit 1
        fi
    fi
    
    print_success "PostgreSQL container is running"
}

# Check Redis container
check_redis() {
    print_info "Checking Redis container..."
    
    # Check if container is running
    if ! docker ps | grep -q dbaas-redis; then
        print_warning "Redis container is not running. Starting it..."
        start_containers
        return
    fi
    
    # Check if Redis is ready
    if ! docker exec dbaas-redis redis-cli ping >/dev/null 2>&1; then
        print_warning "Redis container is not ready. Waiting..."
        sleep 2
        if ! docker exec dbaas-redis redis-cli ping >/dev/null 2>&1; then
            print_warning "Redis container is not responding, but continuing..."
            return
        fi
    fi
    
    print_success "Redis container is running"
}

# Create .env file if it doesn't exist
setup_env() {
    print_info "Checking environment configuration..."
    
    if [ ! -f .env ]; then
        print_warning ".env file not found. Creating from template..."
        
        cat > .env << 'EOF'
# Application Configuration
PORT=8080

# Database Configuration
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

GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret
GOOGLE_REDIRECT_URL=http://localhost:8080/api/v1/auth/google/callback

# Database credential encryption key (used for AES-GCM in utils/crypto.go)
# MUST be the same across restarts for existing credentials to remain usable.
# Use a long, random string in production and keep it secret.
DB_CRED_ENCRYPTION_KEY=change-this-to-a-long-random-secret

# Redis Configuration (for Orchestrator)
REDIS_ADDR=localhost:6379

# Orchestrator Configuration
# Use a different subnet to avoid conflicts with docker-compose network
ORCHESTRATOR_NETWORK_NAME=dbaas-orchestrator-network
ORCHESTRATOR_SUBNET_CIDR=172.30.0.0/16
ORCHESTRATOR_GATEWAY=172.30.0.1
ORCHESTRATOR_MONITOR_INTERVAL=5
EOF
        
        print_success ".env file created. Please update it with your configuration."
        print_warning "IMPORTANT: Change the JWT secrets in production!"
    else
        print_success ".env file exists"
    fi
    
    # Load environment variables
    export $(cat .env | grep -v '^#' | xargs)
}

# Setup database
setup_database() {
    print_info "Setting up database..."
    
    # Load environment variables
    export $(cat .env | grep -v '^#' | xargs)
    
    # Use postgres superuser from docker-compose.yml to create databases
    # The container is configured with POSTGRES_USER=postgres
    local POSTGRES_SUPERUSER="postgres"
    local DB_NAME="${DB_DATABASE}"
    if [ -z "$DB_NAME" ]; then
        print_error "DB_DATABASE environment variable is required"
        exit 1
    fi
    
    # Check if database exists using Docker exec with postgres superuser
    if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw "$DB_NAME"; then
        print_success "Database '$DB_NAME' already exists"
    else
        print_info "Creating database '$DB_NAME'..."
        # Use Docker exec with postgres superuser (can create any database)
        if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "CREATE DATABASE \"$DB_NAME\";" 2>/dev/null; then
            print_success "Database '$DB_NAME' created"
        else
            # Try with local psql if available
            if command_exists psql; then
                # Use postgres password from docker-compose.yml
                PGPASSWORD="postgres" psql -h "$DB_HOST" -p "$DB_PORT" -U "$POSTGRES_SUPERUSER" -c "CREATE DATABASE \"$DB_NAME\";" 2>/dev/null && {
                    print_success "Database '$DB_NAME' created"
                } || {
                    print_error "Failed to create database. Please create it manually:"
                    print_info "  docker exec -it dbaas-postgres psql -U postgres -c 'CREATE DATABASE \"$DB_NAME\";'"
                    exit 1
                }
            else
                print_error "Failed to create database. Please create it manually:"
                print_info "  docker exec -it dbaas-postgres psql -U postgres -c 'CREATE DATABASE \"$DB_NAME\";'"
                exit 1
            fi
        fi
    fi
    
    # If DB_USERNAME is different from postgres, create the user and grant permissions
    if [ -n "$DB_USERNAME" ] && [ "$DB_USERNAME" != "$POSTGRES_SUPERUSER" ]; then
        print_info "Setting up database user '$DB_USERNAME'..."
        # Check if user exists
        if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -tAc "SELECT 1 FROM pg_roles WHERE rolname='$DB_USERNAME'" 2>/dev/null | grep -q 1; then
            print_success "User '$DB_USERNAME' already exists"
        else
            # Create user with password
            local USER_PASSWORD="${DB_PASSWORD:-postgres}"
            if docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "CREATE USER \"$DB_USERNAME\" WITH PASSWORD '$USER_PASSWORD';" 2>/dev/null; then
                print_success "User '$DB_USERNAME' created"
            else
                print_warning "Failed to create user '$DB_USERNAME', but continuing..."
            fi
        fi
        
        # Always grant privileges (even if user already exists, ensure permissions are set)
        print_info "Granting database privileges to '$DB_USERNAME'..."
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -c "GRANT ALL PRIVILEGES ON DATABASE \"$DB_NAME\" TO \"$DB_USERNAME\";" 2>/dev/null || {
            print_warning "Failed to grant database privileges to '$DB_USERNAME', but continuing..."
        }
        
        # Grant schema privileges to the database user (needed for migrations)
        print_info "Granting schema privileges to '$DB_USERNAME'..."
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -d "$DB_NAME" -c "GRANT ALL ON SCHEMA public TO \"$DB_USERNAME\";" 2>/dev/null || {
            print_warning "Failed to grant schema privileges, but continuing..."
        }
        
        # Grant CREATE privilege on the database (needed for creating types)
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -d "$DB_NAME" -c "GRANT CREATE ON DATABASE \"$DB_NAME\" TO \"$DB_USERNAME\";" 2>/dev/null || {
            print_warning "Failed to grant CREATE privilege, but continuing..."
        }
        
        # Make the user owner of the public schema (gives full control)
        docker exec dbaas-postgres psql -U "$POSTGRES_SUPERUSER" -d "$DB_NAME" -c "ALTER SCHEMA public OWNER TO \"$DB_USERNAME\";" 2>/dev/null || {
            print_warning "Failed to set schema owner, but continuing..."
        }
    fi
}

# Install Go dependencies
install_dependencies() {
    print_info "Installing Go dependencies..."
    go mod download
    go mod tidy
    print_success "Go dependencies installed"
}

# Run database migrations
run_migrations() {
    print_info "Running database migrations..."
    
    # Load environment variables
    export $(cat .env | grep -v '^#' | xargs)
    
    # The migrations will run automatically when the server starts
    # But we can also run them manually here if needed
    print_success "Migrations will run automatically on server start"
}

# Build the application
build_app() {
    print_info "Building application..."
    go build -o bin/api cmd/api/main.go
    print_success "Application built successfully"
}

# Stop containers (cleanup function)
stop_containers() {
    print_info "Stopping Docker containers..."
    
    if command_exists docker-compose; then
        COMPOSE_CMD="docker-compose"
    elif docker compose version >/dev/null 2>&1; then
        COMPOSE_CMD="docker compose"
    else
        print_warning "Docker Compose not available, stopping containers manually..."
        docker stop dbaas-postgres dbaas-redis 2>/dev/null || true
        return
    fi
    
    if [ -f docker-compose.yml ]; then
        # Use 'stop' instead of 'down' to keep containers visible in docker ps -a
        $COMPOSE_CMD stop 2>/dev/null || {
            print_warning "Failed to stop containers gracefully. Stopping manually..."
            docker stop dbaas-postgres dbaas-redis 2>/dev/null || true
        }
    fi
}

# Cleanup handler
cleanup() {
    echo ""
    print_info "Shutting down..."
    stop_containers
    exit 0
}

# Register cleanup handler
trap cleanup SIGINT SIGTERM EXIT

# Start the application
start_app() {
    print_info "Starting application..."
    print_info "Server will be available at http://localhost:${PORT:-8080}"
    print_info "Press Ctrl+C to stop the server and containers"
    echo ""
    
    # Load environment variables and start
    export $(cat .env | grep -v '^#' | xargs)
    
    if [ -f bin/api ]; then
        ./bin/api
    else
        go run cmd/api/main.go
    fi
}

# Main execution
main() {
    echo "=========================================="
    echo "  Database-as-a-Service Backend"
    echo "  Startup Script"
    echo "=========================================="
    echo ""
    
    # Change to script directory
    cd "$(dirname "$0")"
    
    # Run checks
    check_go
    check_docker
    start_containers
    check_postgresql
    check_redis
    setup_env
    setup_database
    install_dependencies
    run_migrations
    
    echo ""
    print_success "All dependencies are ready!"
    echo ""
    
    # Start the application
    start_app
}

# Run main function
main

