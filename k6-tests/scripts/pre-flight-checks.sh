#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/pre-flight-checks.sh
# Scenario 10: CLUSTER RESOURCE CHECKS (run before every test)
#
# Validates:
# - Node is Ready
# - CPU < 80% and Memory < 85%
# - All DBaaS pods are Running
# - PVCs are Bound
# - Disk usage < 80%
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

NAMESPACE="${1:-default}"
ABORT=false

info()  { echo -e "${BLUE}[CHECK]${NC} $1"; }
pass()  { echo -e "${GREEN}[PASS]${NC}  $1"; }
fail()  { echo -e "${RED}[FAIL]${NC}  $1"; ABORT=true; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $1"; }

echo ""
echo -e "${BOLD}╔══════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   k6 Pre-flight Cluster Checks       ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════╝${NC}"
echo ""

# ── 1. Node Ready ────────────────────────────────────────────────────────────
info "Checking node status..."
NODE_STATUS=$(kubectl get nodes --no-headers 2>/dev/null | awk '{print $2}')
if echo "$NODE_STATUS" | grep -q "Ready"; then
  pass "Node is Ready"
else
  fail "Node is NOT Ready (status: $NODE_STATUS)"
fi

# ── 2. metrics-server ───────────────────────────────────────────────────────
info "Checking metrics-server..."
if kubectl get deployment metrics-server -n kube-system &>/dev/null 2>&1; then
  READY=$(kubectl get deployment metrics-server -n kube-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
  if [ "${READY:-0}" -gt 0 ]; then
    pass "metrics-server is running"
    METRICS_OK=true
  else
    warn "metrics-server has 0 ready replicas"
    METRICS_OK=false
  fi
else
  warn "metrics-server not installed — CPU/memory checks will use /proc fallback"
  METRICS_OK=false
fi

# ── 3. CPU and Memory headroom ──────────────────────────────────────────────
info "Checking CPU and memory headroom..."
if $METRICS_OK; then
  TOP_OUTPUT=$(kubectl top nodes --no-headers 2>/dev/null || echo "")
  if [ -n "$TOP_OUTPUT" ]; then
    CPU_PCT=$(echo "$TOP_OUTPUT" | awk '{gsub(/%/,"",$3); print $3}')
    MEM_PCT=$(echo "$TOP_OUTPUT" | awk '{gsub(/%/,"",$5); print $5}')
  else
    CPU_PCT=0; MEM_PCT=0
  fi
else
  # /proc fallback
  IDLE=$(awk '/^cpu / {print $5}' /proc/stat)
  TOTAL=$(awk '/^cpu / {s=0;for(i=2;i<=NF;i++)s+=$i;print s}' /proc/stat)
  sleep 1
  IDLE2=$(awk '/^cpu / {print $5}' /proc/stat)
  TOTAL2=$(awk '/^cpu / {s=0;for(i=2;i<=NF;i++)s+=$i;print s}' /proc/stat)
  if [ $((TOTAL2-TOTAL)) -gt 0 ]; then
    CPU_PCT=$(awk "BEGIN{printf \"%.0f\",(1-($IDLE2-$IDLE)/($TOTAL2-$TOTAL))*100}")
  else
    CPU_PCT=0
  fi
  TOTAL_KB=$(awk '/^MemTotal:/{print $2}' /proc/meminfo)
  AVAIL_KB=$(awk '/^MemAvailable:/{print $2}' /proc/meminfo)
  MEM_PCT=$(awk "BEGIN{printf \"%.0f\",(1-$AVAIL_KB/$TOTAL_KB)*100}")
fi

if [ "$CPU_PCT" -gt 80 ]; then
  fail "CPU usage too high: ${CPU_PCT}% (threshold: 80%)"
else
  pass "CPU usage: ${CPU_PCT}% (< 80%)"
fi

if [ "$MEM_PCT" -gt 85 ]; then
  fail "Memory usage too high: ${MEM_PCT}% (threshold: 85%)"
else
  pass "Memory usage: ${MEM_PCT}% (< 85%)"
fi

# ── 4. Pod status ───────────────────────────────────────────────────────────
info "Checking pod status in namespace '${NAMESPACE}'..."
BAD_PODS=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -v "Running\|Completed" || true)
if [ -n "$BAD_PODS" ]; then
  fail "Some pods are not Running in namespace '${NAMESPACE}':"
  echo "$BAD_PODS" | while read -r line; do echo "        $line"; done
else
  TOTAL_PODS=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | wc -l)
  pass "All ${TOTAL_PODS} pods in '${NAMESPACE}' are Running/Completed"
fi

# Also check operator namespaces
for ns in postgres-operator mongodb-operator; do
  if kubectl get namespace "$ns" &>/dev/null 2>&1; then
    BAD=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | grep -v "Running\|Completed" || true)
    if [ -n "$BAD" ]; then
      fail "Bad pods in namespace '${ns}':"
      echo "$BAD" | while read -r line; do echo "        $line"; done
    else
      COUNT=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | wc -l)
      pass "All ${COUNT} pods in '${ns}' are healthy"
    fi
  fi
done

# ── 5. PVC status ───────────────────────────────────────────────────────────
info "Checking PersistentVolumeClaims..."
ALL_PVC_NS="$NAMESPACE"
UNBOUND_PVC=$(kubectl get pvc --all-namespaces --no-headers 2>/dev/null | grep -v "Bound" || true)
if [ -n "$UNBOUND_PVC" ]; then
  warn "Some PVCs are not Bound:"
  echo "$UNBOUND_PVC" | while read -r line; do echo "        $line"; done
else
  PVC_COUNT=$(kubectl get pvc --all-namespaces --no-headers 2>/dev/null | wc -l)
  pass "All ${PVC_COUNT} PVCs are Bound"
fi

# ── 6. Disk usage ───────────────────────────────────────────────────────────
info "Checking disk usage..."
DISK_PCT=$(df / 2>/dev/null | tail -1 | awk '{gsub(/%/,"",$5); print $5}')
if [ "$DISK_PCT" -gt 80 ]; then
  fail "Disk usage too high: ${DISK_PCT}% (threshold: 80%)"
else
  pass "Disk usage: ${DISK_PCT}% (< 80%)"
fi

# ── Result ──────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if $ABORT; then
  echo -e "${RED}${BOLD}PRE-FLIGHT FAILED${NC} — Fix issues above before running load tests."
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  exit 1
else
  echo -e "${GREEN}${BOLD}PRE-FLIGHT PASSED${NC} — Cluster is ready for load testing."
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  exit 0
fi
