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
        -f deploy/service.yaml \
        -f deploy/ingress.yaml

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

# ─── Push Dashboards ──────────────────────────────────────────────────────────
push_dashboards() {
    print_info "Pushing Grafana dashboards..."
    local GRAFANA_POD
    GRAFANA_POD=$(kubectl get pod -n monitoring -l app.kubernetes.io/name=grafana -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [ -n "$GRAFANA_POD" ]; then
      print_info "Configuring Prometheus datasource..."
      kubectl exec -n monitoring "$GRAFANA_POD" -- sh -c "
        curl -s -X POST http://admin:${GRAFANA_PASSWORD:-admin123}@localhost:3000/api/datasources \
          -H 'Content-Type: application/json' \
          -d '{
            \"name\": \"Prometheus\",
            \"type\": \"prometheus\",
            \"url\": \"http://monitoring-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090\",
            \"access\": \"proxy\",
            \"isDefault\": true,
            \"uid\": \"dfooax2k8gjr4b\"
          }' >/dev/null 2>&1
      " || true

      for dashboard_file in grafana/dashboards/*.json; do
        [ ! -f "$dashboard_file" ] && continue
        local name
        name=$(basename "$dashboard_file" .json)
        local tmp_json="/tmp/$name.json"
        local tmp_payload="/tmp/payload-$name.json"
        kubectl cp "$dashboard_file" "monitoring/$GRAFANA_POD:$tmp_json" >/dev/null 2>&1 || continue
        local http_code
        http_code=$(kubectl exec -n monitoring "$GRAFANA_POD" -- sh -c "
          printf '{\"dashboard\": ' > $tmp_payload && \
          cat $tmp_json >> $tmp_payload && \
          printf ', \"overwrite\": true}' >> $tmp_payload && \
          curl -s -w '%{http_code}' -o /tmp/response-$name.json \
            -X POST http://admin:${GRAFANA_PASSWORD:-admin123}@localhost:3000/api/dashboards/db \
            -H 'Content-Type: application/json' \
            -d @$tmp_payload
        " 2>/dev/null || echo "000")
        http_code="${http_code: -3}"
        if [ "$http_code" = "200" ]; then
          print_success "  $name imported"
        else
          print_warning "  Failed to import $name (HTTP $http_code)"
        fi
      done
    else
      print_warning "Grafana pod not found, skipping dashboard push"
    fi
}

# ─── Port-forward ─────────────────────────────────────────────────────────────
PF_PID=""
GRAFANA_PF_PID=""
PROM_PF_PID=""

start_port_forward() {
    local port="${K8S_PORT:-8080}"
    kubectl port-forward svc/backend "${port}:8080" -n default &
    PF_PID=$!

    # Try to port-forward Grafana if available
    if kubectl get svc/grafana -n monitoring &>/dev/null; then
        kubectl port-forward svc/grafana 3000:80 -n monitoring &
        GRAFANA_PF_PID=$!
        print_success "Grafana listening on http://localhost:3000"
    fi

    # Try to port-forward Prometheus if available
    if kubectl get svc/monitoring-kube-prometheus-prometheus -n monitoring &>/dev/null; then
        kubectl port-forward svc/monitoring-kube-prometheus-prometheus 9090:9090 -n monitoring &
        PROM_PF_PID=$!
        print_success "Prometheus listening on http://localhost:9090"
    fi

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
    [ -n "${PF_PID:-}" ] && kill "$PF_PID" 2>/dev/null && print_info "Backend port-forward stopped"
    [ -n "${GRAFANA_PF_PID:-}" ] && kill "$GRAFANA_PF_PID" 2>/dev/null && print_info "Grafana port-forward stopped"
    [ -n "${PROM_PF_PID:-}" ] && kill "$PROM_PF_PID" 2>/dev/null && print_info "Prometheus port-forward stopped"
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
    push_dashboards
    start_port_forward
    stream_logs
}

main "$@"