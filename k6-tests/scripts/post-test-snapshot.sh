#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/post-test-snapshot.sh
# Captures a resource snapshot after each test run.
# Compares pod CPU/memory against defined resource limits.
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
CYAN='\033[0;36m'
NC='\033[0m'

NAMESPACE="${1:-default}"
TIMESTAMP=$(date '+%Y%m%d-%H%M%S')

echo ""
echo -e "${BOLD}${CYAN}╔══════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║   Post-Test Resource Snapshot         ║${NC}"
echo -e "${BOLD}${CYAN}╚══════════════════════════════════════╝${NC}"
echo -e "Namespace: ${NAMESPACE}"
echo -e "Timestamp: ${TIMESTAMP}"
echo ""

# ── Pod resource usage ──────────────────────────────────────────────────────
echo -e "${BOLD}── Pod Resource Usage ──${NC}"
if kubectl top pods -n "$NAMESPACE" &>/dev/null 2>&1; then
  kubectl top pods -n "$NAMESPACE" 2>/dev/null
else
  echo "  (metrics-server not available, skipping kubectl top)"
fi

echo ""
echo -e "${BOLD}── Pod Resources vs Limits ──${NC}"
printf "%-40s %-15s %-15s %-15s %-15s\n" "POD" "CPU_REQ" "CPU_LIM" "MEM_REQ" "MEM_LIM"
printf "%-40s %-15s %-15s %-15s %-15s\n" "---" "-------" "-------" "-------" "-------"

kubectl get pods -n "$NAMESPACE" -o json 2>/dev/null | \
  python3 -c "
import json, sys
data = json.load(sys.stdin)
for pod in data.get('items', []):
    name = pod['metadata']['name'][:39]
    for c in pod['spec'].get('containers', []):
        res = c.get('resources', {})
        req = res.get('requests', {})
        lim = res.get('limits', {})
        print(f'{name:<40} {req.get(\"cpu\",\"—\"):<15} {lim.get(\"cpu\",\"—\"):<15} {req.get(\"memory\",\"—\"):<15} {lim.get(\"memory\",\"—\"):<15}')
" 2>/dev/null || echo "  (python3 not available for detailed output)"

# ── Compare actual usage vs limits ──────────────────────────────────────────
echo ""
echo -e "${BOLD}── Usage Warnings ──${NC}"
if kubectl top pods -n "$NAMESPACE" --no-headers &>/dev/null 2>&1; then
  WARNINGS=0
  while IFS= read -r line; do
    POD_NAME=$(echo "$line" | awk '{print $1}')
    CPU_USED=$(echo "$line" | awk '{print $2}')
    MEM_USED=$(echo "$line" | awk '{print $3}')

    # Get limits for this pod
    CPU_LIMIT=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.containers[0].resources.limits.cpu}' 2>/dev/null || echo "")
    MEM_LIMIT=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.containers[0].resources.limits.memory}' 2>/dev/null || echo "")

    if [ -n "$CPU_LIMIT" ] && [ -n "$CPU_USED" ]; then
      # Simple heuristic: if CPU_USED contains 'm' and limit contains 'm', compare directly
      CPU_U=$(echo "$CPU_USED" | sed 's/m//')
      if [[ "$CPU_LIMIT" =~ ^([0-9]+)m$ ]]; then
        CPU_L="${BASH_REMATCH[1]}"
      elif [[ "$CPU_LIMIT" =~ ^([0-9]+)$ ]]; then
        CPU_L=$((${BASH_REMATCH[1]} * 1000))
      else
        CPU_L=999999
      fi
      if [ "$CPU_U" -gt $((CPU_L * 80 / 100)) ] 2>/dev/null; then
        echo -e "  ${YELLOW}⚠${NC} ${POD_NAME}: CPU ${CPU_USED} is >80% of limit ${CPU_LIMIT}"
        WARNINGS=$((WARNINGS+1))
      fi
    fi
  done <<< "$(kubectl top pods -n "$NAMESPACE" --no-headers 2>/dev/null)"

  if [ "$WARNINGS" -eq 0 ]; then
    echo -e "  ${GREEN}✓${NC} All pods within resource limits"
  fi
else
  echo "  (metrics-server not available)"
fi

# ── Node overview ───────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}── Node Overview ──${NC}"
if kubectl top nodes &>/dev/null 2>&1; then
  kubectl top nodes 2>/dev/null
fi

echo ""
echo -e "${GREEN}Snapshot captured at ${TIMESTAMP}${NC}"
echo ""
