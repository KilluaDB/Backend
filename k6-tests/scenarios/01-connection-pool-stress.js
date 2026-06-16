// ─────────────────────────────────────────────────────────────────────────────
// k6-tests/scenarios/01-connection-pool-stress.js
// Scenario 1: CONNECTION POOL STRESS
//
// Ramps from 1 → N VUs, each opening DB connections via the API.
// Measures connection establishment time, pool exhaustion, and error rates.
// ─────────────────────────────────────────────────────────────────────────────

import { check, sleep } from "k6";
import { Trend, Counter, Rate } from "k6/metrics";
import { BASE_URL, TEST_EMAIL, TEST_PASSWORD, SLO } from "../lib/config.js";
import {
  loginUser,
  registerUser,
  authHeaders,
  createProject,
  waitForProjectReady,
  deleteProject,
  execSQL,
  getPgDashboardMetrics,
  healthCheck,
  queryMongoDocs,
} from "../lib/helpers.js";

// ── Custom Metrics ──────────────────────────────────────────────────────────
const connectionTime     = new Trend("connection_establish_time_ms", true);
const poolExhaustErrors  = new Counter("pool_exhaustion_errors");
const connectionFailRate = new Rate("connection_fail_rate");
const activeConnections  = new Trend("active_connections_gauge");

// ── k6 Options ──────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    pg_pool_stress: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: 10 },   // warm up
        { duration: "1m",  target: 25 },   // moderate
        { duration: "1m",  target: 50 },   // heavy
        { duration: "1m",  target: 75 },   // approaching limit
        { duration: "1m",  target: 100 },  // peak
        { duration: "30s", target: 0 },    // cool down
      ],
      tags: { scenario: "pg_pool_stress" },
    },
    mongo_pool_stress: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: 10 },
        { duration: "1m",  target: 25 },
        { duration: "1m",  target: 50 },
        { duration: "1m",  target: 75 },
        { duration: "1m",  target: 100 },
        { duration: "30s", target: 0 },
      ],
      tags: { scenario: "mongo_pool_stress" },
    },
  },
  thresholds: {
    connection_establish_time_ms: [`p(95)<${SLO.connection_time_p95}`],
    connection_fail_rate:         [`rate<${SLO.http_req_failed_rate}`],
    http_req_duration:            [`p(95)<${SLO.http_req_duration_p95}`],
    http_req_failed:              [`rate<${SLO.http_req_failed_rate}`],
  },
  tags: { testSuite: "connection_pool_stress" },
};

// ── Setup: create test projects ─────────────────────────────────────────────
export function setup() {
  const auth = registerUser(TEST_EMAIL, TEST_PASSWORD);
  if (!auth) throw new Error("Setup: failed to authenticate test user");

  // Create PG project
  const pgProject = createProject(auth.access_token, "k6-pool-stress-pg", "sql");
  // Create Mongo project
  const mongoProject = createProject(auth.access_token, "k6-pool-stress-mongo", "nosql");

  // Wait for both to be ready
  let pgReady = null, mongoReady = null;
  if (pgProject) pgReady = waitForProjectReady(auth.access_token, pgProject.id);
  if (mongoProject) mongoReady = waitForProjectReady(auth.access_token, mongoProject.id);

  // Seed PG with a test table
  if (pgReady) {
    execSQL(auth.access_token, pgProject.id,
      `CREATE TABLE IF NOT EXISTS k6_pool_test (
        id SERIAL PRIMARY KEY,
        data TEXT DEFAULT 'pool-stress-test',
        created_at TIMESTAMPTZ DEFAULT NOW()
      )`
    );
  }

  return {
    token:          auth.access_token,
    pgProjectId:    pgProject ? pgProject.id : null,
    mongoProjectId: mongoProject ? mongoProject.id : null,
    pgReady:        !!pgReady,
    mongoReady:     !!mongoReady,
  };
}

// ── Default VU function ─────────────────────────────────────────────────────
export default function (data) {
  const scenario = __ENV.K6_SCENARIO_NAME || "pg_pool_stress";

  if (scenario === "pg_pool_stress" && data.pgReady) {
    pgPoolStress(data);
  } else if (scenario === "mongo_pool_stress" && data.mongoReady) {
    mongoPoolStress(data);
  } else {
    // fallback: run PG stress
    if (data.pgReady) pgPoolStress(data);
  }
}

function pgPoolStress(data) {
  const start = Date.now();

  // Simulate a "connection" by executing a simple query
  const res = execSQL(data.token, data.pgProjectId, "SELECT 1 AS alive");

  const elapsed = Date.now() - start;
  connectionTime.add(elapsed);

  const success = check(res, {
    "pg connection OK (200)": (r) => r.status === 200,
  });

  connectionFailRate.add(!success);

  if (!success) {
    poolExhaustErrors.add(1);
    // Check for pool exhaustion signals
    if (res.body && res.body.includes("pool")) {
      poolExhaustErrors.add(1, { reason: "pool_exhausted" });
    }
  }

  // Also fetch dashboard metrics to see active_connections
  const metricsRes = getPgDashboardMetrics(data.token, data.pgProjectId);
  if (metricsRes.status === 200) {
    try {
      const m = metricsRes.json().data;
      if (m && m.active_connections !== undefined) {
        activeConnections.add(m.active_connections);
      }
    } catch (_) { /* ignore parse errors */ }
  }

  sleep(0.1); // small pause between iterations
}

function mongoPoolStress(data) {
  const start = Date.now();

  // Simulate a MongoDB "connection" via a query
  const res = queryMongoDocs(data.token, data.mongoProjectId, "__k6_pool_test", {}, 1, 1);

  const elapsed = Date.now() - start;
  connectionTime.add(elapsed);

  const success = check(res, {
    "mongo connection OK": (r) => r.status === 200 || r.status === 404,
  });

  connectionFailRate.add(!success);
  if (!success) poolExhaustErrors.add(1);

  sleep(0.1);
}

// ── Teardown ────────────────────────────────────────────────────────────────
export function teardown(data) {
  if (data.pgProjectId)    deleteProject(data.token, data.pgProjectId);
  if (data.mongoProjectId) deleteProject(data.token, data.mongoProjectId);
}
