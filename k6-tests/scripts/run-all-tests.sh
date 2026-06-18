#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/run-all-tests.sh
# Master orchestrator: runs all k6 test scenarios in the correct order.
#
# Usage: ./scripts/run-all-tests.sh [--skip-provision] [--quick]
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K6_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RESULTS_DIR="${K6_DIR}/results/$(date +%Y%m%d-%H%M%S)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
CYAN='\033[0;36m'
NC='\033[0m'

SKIP_PROVISION=false
QUICK_MODE=false
for arg in "$@"; do
  case "$arg" in
    --skip-provision) SKIP_PROVISION=true ;;
    --quick) QUICK_MODE=true ;;
  esac
done

mkdir -p "$RESULTS_DIR"

echo ""
echo -e "${BOLD}${CYAN}╔════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║   KilluaDB k6 Load Test Suite                  ║${NC}"
echo -e "${BOLD}${CYAN}║   Results: ${RESULTS_DIR##*/}        ║${NC}"
echo -e "${BOLD}${CYAN}╚════════════════════════════════════════════════╝${NC}"
echo ""

# ── Step 0: Pre-flight checks ──────────────────────────────────────────────
echo -e "${BOLD}━━━ Step 0: Pre-flight Checks ━━━${NC}"
bash "$SCRIPT_DIR/pre-flight-checks.sh" default || {
  echo -e "${RED}Pre-flight failed. Aborting test suite.${NC}"
  exit 1
}

echo -e "${BOLD}━━━ Step 0b: Node Resources ━━━${NC}"
bash "$SCRIPT_DIR/check-node-resources.sh" | tee "$RESULTS_DIR/node-resources-before.txt"

# ── Helper: run a k6 test ──────────────────────────────────────────────────
run_test() {
  local num="$1"
  local name="$2"
  local script="$3"
  local output_file="$RESULTS_DIR/${num}-${name}.json"
  local html_file="$RESULTS_DIR/${num}-${name}-report.html"

  echo ""
  echo -e "${BOLD}━━━ Test ${num}: ${name} ━━━${NC}"
  echo -e "${BLUE}Script:${NC} ${script}"
  echo -e "${BLUE}Output JSON:${NC} ${output_file}"
  echo -e "${BLUE}Output HTML:${NC} ${html_file}"
  echo ""

  # If this is the failover test, launch the pod killer in the background
  if [ "$name" == "failover-recovery" ]; then
    echo -e "${YELLOW}[ACTION]${NC} Launching pod killer in background for failover test..."
    bash "$SCRIPT_DIR/kill-pod.sh" "k6-failover-pg" 60 &
  fi


  if K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_EXPORT="$html_file" k6 run \
    --out json="$output_file" \
    --out influxdb=http://127.0.0.1:8086/myk6db \
    --summary-trend-stats="min,avg,med,p(50),p(90),p(95),p(99),max" \
    --tag testrun="$(date +%s)" \
    "$script" 2>&1 | tee "$RESULTS_DIR/${num}-${name}.log"; then
    echo -e "${GREEN}[PASS]${NC} Test ${num} (${name}) completed"
  else
    echo -e "${YELLOW}[DONE]${NC} Test ${num} (${name}) finished with threshold failures"
  fi

  # Post-test snapshot
  bash "$SCRIPT_DIR/post-test-snapshot.sh" default >> "$RESULTS_DIR/${num}-${name}-snapshot.txt" 2>&1 || true
  echo ""
}

# ── Test Execution Order ───────────────────────────────────────────────────
# Phase 1: Baseline & Performance (sequential — each test creates/destroys its own resources)

run_test "01" "connection-pool-stress" "$K6_DIR/scenarios/01-connection-pool-stress.js"

run_test "02" "query-throughput-latency" "$K6_DIR/scenarios/02-query-throughput-latency.js"

# Phase 2: Endurance
run_test "03" "soak-test" "$K6_DIR/scenarios/03-soak-test.js"

# Phase 3: Burst & Recovery
run_test "04" "spike-test" "$K6_DIR/scenarios/04-spike-test.js"

# Phase 4: Provisioning (can be slow, skip with --skip-provision)
if ! $SKIP_PROVISION; then
  run_test "05" "provisioning-latency" "$K6_DIR/scenarios/05-provisioning-latency.js"
fi

# Phase 5: Resilience
run_test "06" "failover-recovery" "$K6_DIR/scenarios/06-failover-recovery.js"

# Phase 6: Isolation
run_test "07" "multi-tenant-isolation" "$K6_DIR/scenarios/07-multi-tenant-isolation.js"

# Phase 7: I/O contention
run_test "08" "backup-io-impact" "$K6_DIR/scenarios/08-backup-io-impact.js"

# Phase 8: Limits
run_test "09" "resource-ceiling" "$K6_DIR/scenarios/09-resource-ceiling.js"

# ── Final snapshot ──────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}━━━ Final Resource Snapshot ━━━${NC}"
bash "$SCRIPT_DIR/check-node-resources.sh" | tee "$RESULTS_DIR/node-resources-after.txt"
bash "$SCRIPT_DIR/post-test-snapshot.sh" default >> "$RESULTS_DIR/final-snapshot.txt" 2>&1 || true

# ── Summary ─────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${CYAN}╔════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${CYAN}║   Test Suite Complete                          ║${NC}"
echo -e "${BOLD}${CYAN}╚════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Results saved to: ${BOLD}${RESULTS_DIR}${NC}"
echo ""
echo "Files:"
ls -la "$RESULTS_DIR/" 2>/dev/null
echo ""
echo -e "${BLUE}To view results in Grafana, import the k6 dashboard:${NC}"
echo "  1. kubectl port-forward svc/grafana -n monitoring 3000:80"
echo "  2. Open http://localhost:3000"
echo "  3. Import dashboard ID 2587 (k6 Load Testing Results)"
echo ""
