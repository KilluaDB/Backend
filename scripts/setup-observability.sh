#!/usr/bin/env bash
# Setup full observability stack (Prometheus + Grafana + AlertManager) on k3s/k3d.
# Run this AFTER ./start.sh — the backend must be running to be scraped.
# Usage: ./scripts/setup-observability.sh
#
# Environment variables:
#   GRAFANA_PASSWORD  admin password (default: admin123)
#   CLUSTER_NAME      k3d cluster name (default: dbaas-local)

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
err()   { echo -e "${RED}[ERROR]${NC} $1"; }

GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin123}"
CLUSTER_NAME="${CLUSTER_NAME:-dbaas-local}"

cd "$(dirname "$0")/.."

# ─── Prerequisites ─────────────────────────────────────────────────────────────
info "Checking prerequisites..."
for cmd in kubectl helm docker k3d; do
  if ! command -v "$cmd" &>/dev/null; then
    err "$cmd is required. Install it first."
    exit 1
  fi
done
ok "All prerequisites found"

if ! kubectl cluster-info &>/dev/null; then
  err "Kubernetes cluster not reachable. Run ./scripts/k3s-local-setup.sh first."
  exit 1
fi

# ─── 1. Metrics Server ─────────────────────────────────────────────────────────
info "Installing/upgrading metrics-server..."
helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server/ 2>/dev/null || true
helm upgrade --install metrics-server metrics-server/metrics-server \
  --namespace kube-system \
  --set "args[0]=--kubelet-insecure-tls" \
  --wait --timeout 3m
ok "metrics-server ready"

# ─── 2. Pre-pull + import images (avoids cluster DNS issues) ───────────────────
info "Pre-pulling monitoring images (this may take a while)..."
IMAGES=(
  "quay.io/prometheus/prometheus:v3.12.0-distroless"
  "quay.io/prometheus/alertmanager:v0.32.2"
  "quay.io/prometheus/node-exporter:v1.11.1-distroless"
  "quay.io/prometheus-operator/prometheus-operator:v0.91.0"
  "quay.io/prometheus-operator/prometheus-config-reloader:v0.91.0"
  "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.19.0"
  "quay.io/kiwigrid/k8s-sidecar:2.7.3"
  "ghcr.io/jkroepke/kube-webhook-certgen:1.8.3"
  "docker.io/grafana/grafana-oss:11.6.0"
)

for img in "${IMAGES[@]}"; do
  if ! docker image inspect "$img" &>/dev/null; then
    info "  Pulling $img ..."
    docker pull "$img" || warn "  Failed to pull $img (will retry inside cluster)"
  fi
done

info "Importing images into k3d cluster..."
k3d image import "${IMAGES[@]}" -c "$CLUSTER_NAME" 2>&1 || warn "Image import had issues (cluster may pull directly)"
ok "Images ready in cluster"

# ─── 3. Install kube-prometheus-stack (Prometheus + AlertManager) ──────────────
info "Installing kube-prometheus-stack..."
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
helm repo update prometheus-community 2>/dev/null

# Skip Grafana here — we install it separately with a lighter image
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring --create-namespace \
  --set grafana.enabled=false \
  --wait --timeout 10m
ok "kube-prometheus-stack installed"

# ─── 5. Apply PodMonitor and ServiceMonitor ────────────────────────────────────
info "Applying consolidated Prometheus monitoring CRDs..."
kubectl apply -f deploy/prometheus.yaml || warn "Prometheus CRDs apply failed"
ok "Monitoring CRDs applied"

# Removed Grafana installation and Dashboard imports since observability 
# is now handled locally via docker-compose (grafana).

# ─── 9. Verify ─────────────────────────────────────────────────────────────────
echo ""
BACKEND_POD=$(kubectl get pod -n default -l app=backend -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "$BACKEND_POD" ]; then
  info "Verifying backend metrics endpoint..."
  if kubectl exec -n default "$BACKEND_POD" -- wget -q -O- http://localhost:8080/metrics 2>/dev/null | grep -q "backend_http_requests_total"; then
    ok "Backend metrics endpoint is live"
  else
    warn "Backend /metrics not responding"
  fi
fi

echo ""
echo "================================================================"
ok "Kubernetes metrics infrastructure is ready!"
echo ""
echo "  Note: Prometheus and Grafana are now run locally via Docker Compose."
echo "  Please run 'docker-compose up -d' to start your observability stack."
echo "================================================================"
