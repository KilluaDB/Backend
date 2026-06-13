#!/usr/bin/env bash
# Tear down the full observability stack (Prometheus, Grafana, AlertManager, metrics-server).
# This does NOT affect the backend, databases, or k3d cluster itself.
# Usage: ./scripts/teardown-observability.sh

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

PROMPT_TIMEOUT=10

confirm_or_timeout() {
  local msg="$1"
  local answer
  if [ -n "${FORCE:-}" ]; then
    return 0
  fi
  echo -n "$msg [y/N] (auto-skip in ${PROMPT_TIMEOUT}s): "
  read -t "$PROMPT_TIMEOUT" answer || echo ""
  case "${answer:-}" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

# ─── 1. Grafana ────────────────────────────────────────────────────────────────
if helm list -n monitoring 2>/dev/null | grep -q grafana; then
  info "Uninstalling Grafana..."
  helm uninstall grafana -n monitoring || warn "Grafana uninstall had issues"
  ok "Grafana removed"
else
  info "Grafana not installed, skipping"
fi

# ─── 2. kube-prometheus-stack (Prometheus, AlertManager, kube-state-metrics, node-exporter) ──
if helm list -n monitoring 2>/dev/null | grep -q monitoring; then
  if confirm_or_timeout "Uninstall kube-prometheus-stack (Prometheus + AlertManager + all scrapers)?"; then
    info "Uninstalling kube-prometheus-stack..."
    helm uninstall monitoring -n monitoring || warn "Prometheus uninstall had issues"

    # CRDs are not removed by Helm uninstall — delete them manually
    info "Cleaning up Prometheus Operator CRDs..."
    kubectl delete crd \
      alertmanagerconfigs.monitoring.coreos.com \
      alertmanagers.monitoring.coreos.com \
      podmonitors.monitoring.coreos.com \
      probes.monitoring.coreos.com \
      prometheuses.monitoring.coreos.com \
      prometheusrules.monitoring.coreos.com \
      servicemonitors.monitoring.coreos.com \
      thanosrulers.monitoring.coreos.com \
      --ignore-not-found 2>/dev/null
    ok "kube-prometheus-stack and CRDs removed"
  else
    info "Skipping kube-prometheus-stack removal"
  fi
else
  info "kube-prometheus-stack not installed, skipping"
fi

# ─── 3. Namespace cleanup ──────────────────────────────────────────────────────
if confirm_or_timeout "Delete the 'monitoring' namespace entirely?"; then
  # Wait for all pods to terminate
  kubectl delete namespace monitoring --ignore-not-found --wait=false 2>/dev/null || true
  info "Waiting for namespace termination..."
  kubectl wait --for=delete namespace/monitoring --timeout=120s 2>/dev/null || true
  # Force remove if stuck (finalizers)
  if kubectl get namespace monitoring &>/dev/null; then
    warn "Namespace monitoring is stuck, removing finalizers..."
    kubectl get namespace monitoring -o json \
      | jq 'del(.spec.finalizers[]? | select(. == "kubernetes"))' \
      | kubectl replace --raw "/api/v1/namespaces/monitoring/finalize" -f - 2>/dev/null || true
    sleep 3
    kubectl delete namespace monitoring --ignore-not-found --force --grace-period=0 2>/dev/null || true
  fi
  ok "Monitoring namespace deleted"
else
  info "Keeping monitoring namespace"
fi

# ─── 4. Metrics Server ─────────────────────────────────────────────────────────
if confirm_or_timeout "Uninstall metrics-server?"; then
  helm uninstall metrics-server -n kube-system 2>/dev/null || true
  ok "metrics-server removed"
else
  info "Keeping metrics-server"
fi

# ─── 5. Verify ─────────────────────────────────────────────────────────────────
info "Verifying cleanup..."
for release in grafana monitoring; do
  if helm list -n monitoring 2>/dev/null | grep -q "$release"; then
    warn "  $release still present"
  fi
done

echo ""
if kubectl get namespace monitoring &>/dev/null; then
  warn "  namespace 'monitoring' still exists"
else
  ok "  namespace 'monitoring' removed"
fi

BACKEND_POD=$(kubectl get pod -n default -l app=backend -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "$BACKEND_POD" ]; then
  info "Backend is still running. Its /metrics endpoint is still available:"
  info "  kubectl exec -n default $BACKEND_POD -- wget -q -O- http://localhost:8080/metrics | head -20"
fi

echo ""
echo "================================================================"
ok "Observability teardown complete!"
echo ""
echo "  The k3d cluster, backend, and databases are untouched."
echo "  To rebuild: ./scripts/setup-observability.sh"
echo "  To destroy everything (cluster + all):"
echo "    ./scripts/k3d-cluster-delete.sh"
echo "================================================================"
