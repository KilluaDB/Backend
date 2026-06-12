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
if kubectl get deployment metrics-server -n kube-system &>/dev/null; then
  ok "metrics-server already deployed"
else
  helm upgrade --install metrics-server metrics-server/metrics-server \
    --namespace kube-system \
    --set "args[0]=--kubelet-insecure-tls" \
    --wait --timeout 3m
  ok "metrics-server ready"
fi

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

# ─── 4. Install Grafana separately ────────────────────────────────────────────
info "Installing Grafana..."
GRAFANA_REPO="grafana"
GRAFANA_TAG="11.6.0"
GRAFANA_IMG="docker.io/grafana/grafana-oss:${GRAFANA_TAG}"
if docker image inspect "grafana/grafana-oss:${GRAFANA_TAG}" &>/dev/null; then
  GRAFANA_REPO="grafana/grafana-oss"
elif docker image inspect "grafana/grafana:${GRAFANA_TAG}" &>/dev/null; then
  GRAFANA_REPO="grafana/grafana"
fi

helm repo add grafana https://grafana.github.io/helm-charts 2>/dev/null || true
helm upgrade --install grafana grafana/grafana \
  --namespace monitoring \
  --set adminPassword="$GRAFANA_PASSWORD" \
  --set persistence.enabled=false \
  --set image.repository="$GRAFANA_REPO" \
  --set image.tag="$GRAFANA_TAG" \
  --wait --timeout 5m
ok "Grafana installed"

# ─── 5. Apply PodMonitor and ServiceMonitor ────────────────────────────────────
info "Applying consolidated Prometheus monitoring CRDs..."
kubectl apply -f deploy/prometheus.yaml || warn "Prometheus CRDs apply failed"
ok "Monitoring CRDs applied"

# ─── 6. Wait for all pods ──────────────────────────────────────────────────────
info "Waiting for all monitoring pods to be ready..."
kubectl wait --for=condition=ready pod -n monitoring --all --timeout=300s || warn "Some pods not ready yet"

# ─── 7. Configure Prometheus datasource in Grafana ─────────────────────────────
info "Configuring Prometheus datasource in Grafana..."
GRAFANA_POD=$(kubectl get pod -n monitoring -l app.kubernetes.io/name=grafana -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "$GRAFANA_POD" ]; then
  # Delete existing if any to ensure clean state with correct UID
  kubectl exec -n monitoring "$GRAFANA_POD" -- sh -c "
    curl -s -X DELETE http://admin:$GRAFANA_PASSWORD@localhost:3000/api/datasources/name/Prometheus >/dev/null 2>&1
  "
  
  # Recreate with the exact UID (dfooax2k8gjr4b) that the dashboards are hardcoded to use
  kubectl exec -n monitoring "$GRAFANA_POD" -- sh -c "
    curl -s -X POST http://admin:$GRAFANA_PASSWORD@localhost:3000/api/datasources \
      -H 'Content-Type: application/json' \
      -d '{
        \"name\": \"Prometheus\",
        \"type\": \"prometheus\",
        \"url\": \"http://monitoring-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090\",
        \"access\": \"proxy\",
        \"isDefault\": true,
        \"uid\": \"dfooax2k8gjr4b\"
      }' >/dev/null 2>&1
  " && ok "  Prometheus datasource configured with UID dfooax2k8gjr4b" || warn "  Failed to add Prometheus datasource"
else
  warn "Grafana pod not found, skipping datasource config"
fi

# ─── 8. Import Grafana dashboards via API ──────────────────────────────────────
info "Importing Grafana dashboards..."
GRAFANA_POD=$(kubectl get pod -n monitoring -l app.kubernetes.io/name=grafana -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "$GRAFANA_POD" ]; then
  for dashboard_file in grafana/dashboards/*.json; do
    [ ! -f "$dashboard_file" ] && { warn "  $dashboard_file not found, skipping"; continue; }
    name=$(basename "$dashboard_file" .json)
    info "  Importing $name ..."
    tmp_json="/tmp/$name.json"
    tmp_payload="/tmp/payload-$name.json"
    kubectl cp "$dashboard_file" "monitoring/$GRAFANA_POD:$tmp_json" >/dev/null 2>&1 || {
      warn "  Failed to copy $name to pod, skipping"
      continue
    }
    response=$(kubectl exec -n monitoring "$GRAFANA_POD" -- sh -c "
      printf '{\"dashboard\": ' > $tmp_payload && \
      cat $tmp_json >> $tmp_payload && \
      printf ', \"overwrite\": true}' >> $tmp_payload && \
      curl -s -w '%{http_code}' -o /tmp/response-$name.json \
        -X POST http://admin:$GRAFANA_PASSWORD@localhost:3000/api/dashboards/db \
        -H 'Content-Type: application/json' \
        -d @$tmp_payload
    " 2>/dev/null || echo "000")
    http_code="${response: -3}"
    if [ "$http_code" = "200" ]; then
      ok "  $name imported"
    else
      warn "  Failed to import $name (HTTP $http_code)"
    fi
  done
else
  warn "Grafana pod not found, skipping dashboard import"
fi

# ─── 9. Verify ─────────────────────────────────────────────────────────────────
echo ""
BACKEND_POD=$(kubectl get pod -n default -l app=backend -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "$BACKEND_POD" ]; then
  info "Verifying backend metrics endpoint..."
  if kubectl exec -n default "$BACKEND_POD" -- wget -q -O- http://localhost:9090/metrics 2>/dev/null | grep -q "backend_"; then
    ok "Backend metrics endpoint is live"
  else
    warn "Backend /metrics not responding"
  fi
fi

echo ""
echo "================================================================"
ok "Observability stack is ready!"
echo ""
echo "  Access Grafana:"
echo "    kubectl port-forward svc/grafana -n monitoring 3000:80"
echo "    URL: http://localhost:3000"
echo "    User: admin"
echo "    Pass: $GRAFANA_PASSWORD"
echo ""
echo "  Check Prometheus targets:"
echo "    kubectl port-forward svc/monitoring-kube-prometheus-prometheus -n monitoring 9090:9090"
echo ""
echo "================================================================"
