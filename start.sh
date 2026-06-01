#!/bin/bash

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

# ─── Load .env ────────────────────────────────────────────────────────────────
load_env() {
    [ ! -f .env ] && { print_error ".env not found"; exit 1; }
    set +o nounset
    export $(grep -v '^\s*#' .env | grep -v '^\s*$' | xargs 2>/dev/null) || true
    set -o nounset
    print_success ".env loaded"
}

# ─── Namespaces ───────────────────────────────────────────────────────────────
ensure_namespaces() {
    for ns in postgres-instances mongodb-instances; do
        kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
    done
    print_success "Namespaces ready"
}

# ─── Meta DBs ─────────────────────────────────────────────────────────────────
deploy_meta_dbs() {
    print_info "Deploying PostgreSQL and Redis..."

    kubectl create secret generic postgres-secrets \
        --from-literal=POSTGRES_PASSWORD="${DB_PASSWORD:-postgres}" \
        -n default --dry-run=client -o yaml | kubectl apply -f -

    kubectl apply -f deploy/postgres.yaml -f deploy/redis.yaml

    kubectl wait --for=condition=ready pod -l app=postgres -n default --timeout=120s
    kubectl wait --for=condition=ready pod -l app=redis   -n default --timeout=60s \
        || print_warning "Redis not ready yet — continuing"

    print_success "PostgreSQL and Redis ready"
}

# ─── ConfigMap ────────────────────────────────────────────────────────────────
apply_configmap() {
   kubectl apply -f deploy/configmap.yaml
    print_success "ConfigMap applied"
}

# ─── Secret ───────────────────────────────────────────────────────────────────
apply_secret() {
    kubectl create secret generic backend-secrets \
        --from-literal=DB_PASSWORD="${DB_PASSWORD:-postgres}" \
        --from-literal=DB_ADMIN_PASSWORD="${DB_ADMIN_PASSWORD:-postgres}" \
        --from-literal=ACCESS_TOKEN_SECRET="${ACCESS_TOKEN_SECRET}" \
        --from-literal=REFRESH_TOKEN_SECRET="${REFRESH_TOKEN_SECRET}" \
        --from-literal=DB_CRED_ENCRYPTION_KEY="${DB_CRED_ENCRYPTION_KEY}" \
        --from-literal=GOOGLE_CLIENT_ID="${GOOGLE_CLIENT_ID:-}" \
        --from-literal=GOOGLE_CLIENT_SECRET="${GOOGLE_CLIENT_SECRET:-}" \
        -n default --dry-run=client -o yaml | kubectl apply -f -
    print_success "Secret applied"
}

# ─── Build & import image ─────────────────────────────────────────────────────
build_image() {
    print_info "Building backend-api:latest..."
    docker build -t backend-api:latest .

    local ctx
    ctx=$(kubectl config current-context 2>/dev/null || true)
    if echo "$ctx" | grep -q '^k3d-'; then
        k3d image import backend-api:latest -c "${ctx#k3d-}"
        print_success "Image imported into k3d"
    fi
}

# ─── Deploy backend ───────────────────────────────────────────────────────────
deploy_backend() {
    kubectl apply \
        -f deploy/rbac.yaml \
        -f deploy/deployment.yaml \
        -f deploy/service.yaml

    kubectl rollout restart deployment/backend -n default
    kubectl rollout status  deployment/backend -n default --timeout=120s
    print_success "Backend deployed"
}

# ─── Deploy pgproxy ───────────────────────────────────────────────────────────
# PostgreSQL SNI routing proxy for external DB access. Runs the /pgproxy binary
# from the same backend-api image, so it relies on build_image having imported
# the freshly built image into k3d first. The Deployment, Service and the
# HostSNI(*) IngressRouteTCP all live in deploy/pgproxy.yaml. The Traefik
# `postgres` entrypoint it routes through is created by scripts/k3s-local-setup.sh
# (deploy/traefik-tcp-config.yaml).
deploy_pgproxy() {
    kubectl apply -f deploy/pgproxy.yaml

    kubectl rollout restart deployment/pgproxy -n default
    kubectl rollout status  deployment/pgproxy -n default --timeout=120s
    print_success "pgproxy deployed (external DB access via Traefik :5432)"
}

# ─── Port-forward ─────────────────────────────────────────────────────────────
PF_PID=""

start_port_forward() {
    local port="${K8S_PORT:-8080}"
    kubectl port-forward svc/backend "${port}:8080" -n default &
    PF_PID=$!
    sleep 1
    kill -0 "$PF_PID" 2>/dev/null || { print_error "Port-forward failed"; exit 1; }
    print_success "Listening on http://localhost:$port"
}

# ─── Logs ─────────────────────────────────────────────────────────────────────
stream_logs() {
    print_info "Streaming logs (Ctrl+C to stop)..."
    while true; do
        kubectl logs -f deploy/backend -n default --all-containers --prefix 2>&1 || true
        sleep 3
    done
}

# ─── Cleanup ──────────────────────────────────────────────────────────────────
cleanup() {
    echo ""
    [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null && print_info "Port-forward stopped"
    print_info "Cluster still running. Bye!"
    exit 0
}

trap cleanup SIGINT SIGTERM EXIT

# ─── Main ─────────────────────────────────────────────────────────────────────
main() {
    echo "=================================="
    echo "  DBaaS Backend — k3d"
    echo "=================================="

    cd "$(dirname "$0")"

    load_env
    ensure_namespaces
    apply_configmap
    apply_secret
    deploy_meta_dbs
    build_image
    deploy_backend
    deploy_pgproxy
    start_port_forward
    stream_logs
}

main "$@"