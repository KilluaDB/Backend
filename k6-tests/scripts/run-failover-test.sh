#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/run-failover-test.sh
# Automates running the failover test and the kill-pod script together.
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K6_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "Starting failover test automation..."

# 1. Launch the kill-pod script in the background. 
# It will sleep for 60 seconds, then kill a pod in the 'k6-failover-pg' namespace.
bash "$SCRIPT_DIR/kill-pod.sh" k6-failover-pg 60 &

# 2. Run the k6 test in the foreground
k6 run "$K6_DIR/scenarios/06-failover-recovery.js"

echo "Failover test complete."
