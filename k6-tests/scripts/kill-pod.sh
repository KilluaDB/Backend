#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/kill-pod.sh
# Companion script for scenario 06 (failover-recovery)
# Kills a specific DB pod to simulate a crash during load testing.
#
# Usage: ./scripts/kill-pod.sh <namespace-pattern> [delay-seconds]
# Example: ./scripts/kill-pod.sh pg- 60
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NS_PATTERN="${1:-pg-}"
DELAY="${2:-60}"

echo -e "${BLUE}[INFO]${NC} Will kill a pod matching namespace pattern '${NS_PATTERN}' in ${DELAY}s..."
sleep "$DELAY"

# Find namespaces matching the pattern
NAMESPACES=$(kubectl get namespaces --no-headers 2>/dev/null | awk '{print $1}' | grep "$NS_PATTERN" || true)

if [ -z "$NAMESPACES" ]; then
  echo -e "${YELLOW}[WARN]${NC} No namespaces found matching pattern '${NS_PATTERN}'"
  exit 0
fi

for ns in $NAMESPACES; do
  # Find the first running pod in this namespace
  POD=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | grep "Running" | head -1 | awk '{print $1}')
  if [ -n "$POD" ]; then
    echo -e "${RED}[KILL]${NC} Deleting pod ${POD} in namespace ${ns} (--grace-period=0 --force)"
    kubectl delete pod "$POD" -n "$ns" --grace-period=0 --force 2>/dev/null || true
    echo -e "${GREEN}[DONE]${NC} Pod deleted. Kubernetes should recreate it."

    # Wait and check recovery
    echo -e "${BLUE}[INFO]${NC} Waiting 30s for pod recovery..."
    sleep 30
    echo -e "${BLUE}[INFO]${NC} Pod status after recovery:"
    kubectl get pods -n "$ns" 2>/dev/null
    break
  fi
done
