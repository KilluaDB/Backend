// ─────────────────────────────────────────────────────────────────────────────
// k6-tests/lib/config.js — Centralized configuration for all k6 test scripts
// ─────────────────────────────────────────────────────────────────────────────

// Base URL for the KilluaDB backend API.
// Override at runtime:  k6 run -e BASE_URL=http://... script.js
export const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// ── Auth credentials ────────────────────────────────────────────────────────
// Pre-existing test user (must be registered before running tests)
export const TEST_EMAIL    = __ENV.TEST_EMAIL    || "k6loadtest@example.com";
export const TEST_PASSWORD = __ENV.TEST_PASSWORD || "K6LoadTest!2026";

// Second tenant for multi-tenant isolation tests
export const TENANT_B_EMAIL    = __ENV.TENANT_B_EMAIL    || "k6tenant-b@example.com";
export const TENANT_B_PASSWORD = __ENV.TENANT_B_PASSWORD || "K6TenantB!2026";

// ── Project / DB identifiers ────────────────────────────────────────────────
// These are set by the setup() phase of each test or can be overridden.
export const PG_PROJECT_ID    = __ENV.PG_PROJECT_ID    || "";
export const MONGO_PROJECT_ID = __ENV.MONGO_PROJECT_ID || "";

// ── SLO thresholds (used across all tests) ──────────────────────────────────
export const SLO = {
  // API-level
  http_req_duration_p95: 200,   // ms
  http_req_duration_p99: 500,   // ms
  http_req_failed_rate:  0.01,  // 1%

  // DB-level
  query_latency_p95:     200,   // ms
  query_latency_p99:     500,   // ms
  connection_time_p95:   100,   // ms

  // Provisioning
  provisioning_time_max: 300,   // seconds (5 min)
};

// ── Shared k6 threshold definitions ─────────────────────────────────────────
export const DEFAULT_THRESHOLDS = {
  http_req_failed:        [{ threshold: `rate<${SLO.http_req_failed_rate}`, abortOnFail: false }],
  http_req_duration:      [`p(95)<${SLO.http_req_duration_p95}`, `p(99)<${SLO.http_req_duration_p99}`],
};

// ── k3s / namespace helpers ─────────────────────────────────────────────────
export const K3S_NAMESPACE      = __ENV.K3S_NAMESPACE      || "default";
export const MONITORING_NS      = __ENV.MONITORING_NS      || "monitoring";
export const PROMETHEUS_URL     = __ENV.PROMETHEUS_URL     || "http://localhost:9090";
export const GRAFANA_URL        = __ENV.GRAFANA_URL        || "http://localhost:3000";
