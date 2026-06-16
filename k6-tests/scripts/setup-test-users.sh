#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# k6-tests/scripts/setup-test-users.sh
# Creates test users required by the k6 test suite.
# Run ONCE before the first test execution.
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[OK]${NC}   $1"; }
err()  { echo -e "${RED}[FAIL]${NC} $1"; }

register_user() {
  local email="$1"
  local password="$2"
  info "Registering ${email}..."
  HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${BASE_URL}/api/v1/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"password\":\"${password}\"}")

  if [ "$HTTP_CODE" = "201" ]; then
    ok "Registered ${email}"
  elif [ "$HTTP_CODE" = "409" ]; then
    ok "Already exists: ${email}"
  else
    err "Failed to register ${email} (HTTP ${HTTP_CODE})"
  fi
}

echo ""
echo "Setting up k6 test users at ${BASE_URL}..."
echo ""

register_user "k6loadtest@example.com"   "K6LoadTest!2026"
register_user "k6tenant-b@example.com"   "K6TenantB!2026"

echo ""
ok "Test users ready."
echo ""
