#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/check-node-resources.sh
# Scenario 11: Check available resources on the k3s single-node cluster
#
# Requirements:
# - kubectl with access to the k3s cluster
# - metrics-server installed for kubectl top
# - Standard Linux tools: df, free, awk, grep
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
err()   { echo -e "${RED}[ERROR]${NC} $1"; }

# ── Check prerequisites ─────────────────────────────────────────────────────
if ! command -v kubectl &>/dev/null; then
  err "kubectl not found. Please install kubectl."
  exit 1
fi

if ! kubectl cluster-info &>/dev/null 2>&1; then
  err "Kubernetes cluster not reachable. Is k3s running?"
  exit 1
fi

# ── Check metrics-server ────────────────────────────────────────────────────
echo ""
info "Checking metrics-server..."
METRICS_SERVER_RUNNING=false
if kubectl get deployment metrics-server -n kube-system &>/dev/null 2>&1; then
  READY=$(kubectl get deployment metrics-server -n kube-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
  if [ "${READY:-0}" -gt 0 ]; then
    METRICS_SERVER_RUNNING=true
    ok "metrics-server is running (${READY} ready replicas)"
  else
    warn "metrics-server deployment exists but has 0 ready replicas"
  fi
else
  warn "⚠ metrics-server is NOT installed!"
  warn "  Install it with: helm install metrics-server metrics-server/metrics-server --namespace kube-system --set args[0]=--kubelet-insecure-tls"
  warn "  Or run: ./scripts/setup-observability.sh"
  warn ""
  warn "  Without metrics-server, 'kubectl top' commands will not work."
  warn "  CPU and Memory usage data will be estimated from /proc instead."
fi

# ── Helper: human readable bytes ─────────────────────────────────────────────
human_bytes() {
  local bytes=$1
  if [ "$bytes" -ge 1073741824 ]; then
    echo "$(awk "BEGIN {printf \"%.1f\", $bytes/1073741824}")Gi"
  elif [ "$bytes" -ge 1048576 ]; then
    echo "$(awk "BEGIN {printf \"%.0f\", $bytes/1048576}")Mi"
  else
    echo "${bytes}B"
  fi
}

# ── Get node name ───────────────────────────────────────────────────────────
NODE_NAME=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -z "$NODE_NAME" ]; then
  err "Could not determine node name"
  exit 1
fi

# ── CPU Information ──────────────────────────────────────────────────────────
# Total CPU from node capacity
TOTAL_CPU_MILLI=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.capacity.cpu}' 2>/dev/null)
# Convert to millicores if given in cores
if [[ "$TOTAL_CPU_MILLI" =~ ^[0-9]+$ ]]; then
  TOTAL_CPU_CORES=$TOTAL_CPU_MILLI
  TOTAL_CPU_MILLI=$((TOTAL_CPU_MILLI * 1000))
elif [[ "$TOTAL_CPU_MILLI" =~ ^([0-9]+)m$ ]]; then
  TOTAL_CPU_MILLI="${BASH_REMATCH[1]}"
  TOTAL_CPU_CORES=$(awk "BEGIN {printf \"%.1f\", $TOTAL_CPU_MILLI/1000}")
fi

# Used CPU
USED_CPU_MILLI=0
USED_CPU_CORES="?"
if $METRICS_SERVER_RUNNING; then
  # kubectl top nodes output: NAME   CPU(cores)   CPU%   MEMORY(bytes)   MEMORY%
  TOP_OUTPUT=$(kubectl top nodes --no-headers 2>/dev/null || echo "")
  if [ -n "$TOP_OUTPUT" ]; then
    USED_CPU_RAW=$(echo "$TOP_OUTPUT" | awk '{print $2}')
    # Parse: could be "2100m" or "2"
    if [[ "$USED_CPU_RAW" =~ ^([0-9]+)m$ ]]; then
      USED_CPU_MILLI="${BASH_REMATCH[1]}"
    elif [[ "$USED_CPU_RAW" =~ ^([0-9]+)$ ]]; then
      USED_CPU_MILLI=$((${BASH_REMATCH[1]} * 1000))
    fi
    USED_CPU_CORES=$(awk "BEGIN {printf \"%.1f\", $USED_CPU_MILLI/1000}")
  fi
else
  # Fallback: read from /proc/stat
  # This is a snapshot, not averaged like kubectl top
  IDLE=$(awk '/^cpu / {print $5}' /proc/stat)
  TOTAL=$(awk '/^cpu / {sum=0; for(i=2;i<=NF;i++) sum+=$i; print sum}' /proc/stat)
  sleep 1
  IDLE2=$(awk '/^cpu / {print $5}' /proc/stat)
  TOTAL2=$(awk '/^cpu / {sum=0; for(i=2;i<=NF;i++) sum+=$i; print sum}' /proc/stat)
  IDLE_DIFF=$((IDLE2 - IDLE))
  TOTAL_DIFF=$((TOTAL2 - TOTAL))
  if [ "$TOTAL_DIFF" -gt 0 ]; then
    CPU_USED_PCT=$(awk "BEGIN {printf \"%.0f\", (1 - $IDLE_DIFF/$TOTAL_DIFF) * 100}")
    USED_CPU_MILLI=$(awk "BEGIN {printf \"%.0f\", $TOTAL_CPU_MILLI * (1 - $IDLE_DIFF/$TOTAL_DIFF)}")
    USED_CPU_CORES=$(awk "BEGIN {printf \"%.1f\", $USED_CPU_MILLI/1000}")
  fi
fi

FREE_CPU_MILLI=$((TOTAL_CPU_MILLI - USED_CPU_MILLI))
FREE_CPU_CORES=$(awk "BEGIN {printf \"%.1f\", $FREE_CPU_MILLI/1000}")
CPU_PCT=$(awk "BEGIN {printf \"%.0f\", $USED_CPU_MILLI/$TOTAL_CPU_MILLI * 100}")

# ── Memory Information ───────────────────────────────────────────────────────
TOTAL_MEM_KI=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.capacity.memory}' 2>/dev/null)
# Convert Ki to bytes
if [[ "$TOTAL_MEM_KI" =~ ^([0-9]+)Ki$ ]]; then
  TOTAL_MEM_BYTES=$((${BASH_REMATCH[1]} * 1024))
elif [[ "$TOTAL_MEM_KI" =~ ^([0-9]+)$ ]]; then
  TOTAL_MEM_BYTES=$TOTAL_MEM_KI
fi

USED_MEM_BYTES=0
if $METRICS_SERVER_RUNNING && [ -n "$TOP_OUTPUT" ]; then
  USED_MEM_RAW=$(echo "$TOP_OUTPUT" | awk '{print $4}')
  if [[ "$USED_MEM_RAW" =~ ^([0-9]+)Mi$ ]]; then
    USED_MEM_BYTES=$((${BASH_REMATCH[1]} * 1048576))
  elif [[ "$USED_MEM_RAW" =~ ^([0-9]+)Gi$ ]]; then
    USED_MEM_BYTES=$((${BASH_REMATCH[1]} * 1073741824))
  elif [[ "$USED_MEM_RAW" =~ ^([0-9]+)$ ]]; then
    USED_MEM_BYTES=$USED_MEM_RAW
  fi
else
  # Fallback: /proc/meminfo
  TOTAL_MEM_KB=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
  AVAIL_MEM_KB=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
  TOTAL_MEM_BYTES=$((TOTAL_MEM_KB * 1024))
  USED_MEM_BYTES=$(( (TOTAL_MEM_KB - AVAIL_MEM_KB) * 1024 ))
fi

FREE_MEM_BYTES=$((TOTAL_MEM_BYTES - USED_MEM_BYTES))
MEM_PCT=$(awk "BEGIN {printf \"%.0f\", $USED_MEM_BYTES/$TOTAL_MEM_BYTES * 100}")

# ── Disk Information ─────────────────────────────────────────────────────────
# Use the root filesystem or the primary mount
DISK_LINE=$(df -B1 / 2>/dev/null | tail -1)
TOTAL_DISK=$(echo "$DISK_LINE" | awk '{print $2}')
USED_DISK=$(echo "$DISK_LINE" | awk '{print $3}')
FREE_DISK=$(echo "$DISK_LINE" | awk '{print $4}')
DISK_PCT=$(echo "$DISK_LINE" | awk '{gsub(/%/,"",$5); print $5}')

# ── Pod Information ──────────────────────────────────────────────────────────
MAX_PODS=$(kubectl get node "$NODE_NAME" -o jsonpath='{.status.capacity.pods}' 2>/dev/null || echo "110")
RUNNING_PODS=$(kubectl get pods --all-namespaces --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
FREE_PODS=$((MAX_PODS - RUNNING_PODS))
POD_PCT=$(awk "BEGIN {printf \"%.0f\", $RUNNING_PODS/$MAX_PODS * 100}")

# ── Output ───────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}=== k3s Node Resource Availability ===${NC}"
echo -e "${BOLD}Node:${NC} ${NODE_NAME}"
echo -e "${BOLD}Time:${NC} $(date '+%Y-%m-%d %H:%M:%S %Z')"
echo ""
printf "  ${BOLD}%-8s${NC}: %s / %s cores   (%s%% used, %s cores free)\n" \
  "CPU" "$USED_CPU_CORES" "$TOTAL_CPU_CORES" "$CPU_PCT" "$FREE_CPU_CORES"
printf "  ${BOLD}%-8s${NC}: %s / %s    (%s%% used, %s free)\n" \
  "Memory" "$(human_bytes $USED_MEM_BYTES)" "$(human_bytes $TOTAL_MEM_BYTES)" "$MEM_PCT" "$(human_bytes $FREE_MEM_BYTES)"
printf "  ${BOLD}%-8s${NC}: %s / %s       (%s%% used, %s free)\n" \
  "Disk" "$(human_bytes $USED_DISK)" "$(human_bytes $TOTAL_DISK)" "$DISK_PCT" "$(human_bytes $FREE_DISK)"
printf "  ${BOLD}%-8s${NC}: %s / %s         (%s%% used, %s slots free)\n" \
  "Pods" "$RUNNING_PODS" "$MAX_PODS" "$POD_PCT" "$FREE_PODS"
echo ""
echo -e "${BOLD}${CYAN}======================================${NC}"

# ── Warnings ─────────────────────────────────────────────────────────────────
WARNINGS=0
if [ "$CPU_PCT" -gt 80 ]; then
  warn "CPU usage is above 80% ($CPU_PCT%). Load tests may be unreliable."
  WARNINGS=$((WARNINGS+1))
fi
if [ "$MEM_PCT" -gt 85 ]; then
  warn "Memory usage is above 85% ($MEM_PCT%). Risk of OOM kills during tests."
  WARNINGS=$((WARNINGS+1))
fi
if [ "$DISK_PCT" -gt 80 ]; then
  warn "Disk usage is above 80% ($DISK_PCT%). Backup/export tests may fail."
  WARNINGS=$((WARNINGS+1))
fi
if [ "$RUNNING_PODS" -gt $((MAX_PODS * 90 / 100)) ]; then
  warn "Pod count is above 90% capacity ($RUNNING_PODS/$MAX_PODS)."
  WARNINGS=$((WARNINGS+1))
fi

if [ "$WARNINGS" -eq 0 ]; then
  echo ""
  ok "All resource levels are within safe thresholds for load testing."
fi
echo ""
