// ─────────────────────────────────────────────────────────────────────────────
// k6-tests/scenarios/02-query-throughput-latency.js
// Scenario 2: QUERY THROUGHPUT & LATENCY
//
// 80% reads / 20% writes workload mix.
// Tests: simple SELECT, JOIN queries, INSERT/UPDATE under varying load.
// Measures p50, p90, p95, p99 query latencies.
// ─────────────────────────────────────────────────────────────────────────────

import { check, sleep, group } from "k6";
import { Trend, Counter, Rate } from "k6/metrics";
import { BASE_URL, TEST_EMAIL, TEST_PASSWORD, SLO } from "../lib/config.js";
import {
  registerUser,
  loginUser,
  authHeaders,
  createProject,
  waitForProjectReady,
  deleteProject,
  execSQL,
  insertPgRow,
  getPgRows,
  insertMongoDocs,
  queryMongoDocs,
  getMongoDocs,
  updateMongoDocs,
} from "../lib/helpers.js";

// ── Custom Metrics ──────────────────────────────────────────────────────────
const pgReadLatency    = new Trend("pg_read_latency_ms",  true);
const pgWriteLatency   = new Trend("pg_write_latency_ms", true);
const pgJoinLatency    = new Trend("pg_join_latency_ms",  true);
const mongoReadLatency  = new Trend("mongo_read_latency_ms",  true);
const mongoWriteLatency = new Trend("mongo_write_latency_ms", true);
const queryErrors       = new Counter("query_errors_total");
const querySuccessRate  = new Rate("query_success_rate");

// ── k6 Options ──────────────────────────────────────────────────────────────
export const options = {
  scenarios: {
    mixed_workload: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: 5 },    // warm up
        { duration: "1m",  target: 15 },   // light load
        { duration: "2m",  target: 30 },   // moderate
        { duration: "2m",  target: 50 },   // heavy
        { duration: "1m",  target: 75 },   // peak
        { duration: "30s", target: 0 },    // cool down
      ],
    },
  },
  thresholds: {
    pg_read_latency_ms:    ["p(50)<50",  "p(90)<100", "p(95)<200", "p(99)<500"],
    pg_write_latency_ms:   ["p(50)<100", "p(90)<200", "p(95)<300", "p(99)<800"],
    pg_join_latency_ms:    ["p(50)<100", "p(90)<250", "p(95)<400", "p(99)<1000"],
    mongo_read_latency_ms: ["p(50)<50",  "p(90)<100", "p(95)<200", "p(99)<500"],
    mongo_write_latency_ms:["p(50)<100", "p(90)<200", "p(95)<300", "p(99)<800"],
    query_success_rate:    ["rate>0.99"],
    http_req_duration:     [`p(95)<${SLO.http_req_duration_p95}`],
    http_req_failed:       [`rate<${SLO.http_req_failed_rate}`],
  },
  tags: { testSuite: "query_throughput_latency" },
  setupTimeout: "5m",
};

// ── Setup ───────────────────────────────────────────────────────────────────
export function setup() {
  const auth = registerUser(TEST_EMAIL, TEST_PASSWORD);
  if (!auth) throw new Error("Setup: auth failed");

  const pgProject = createProject(auth.access_token, "k6-query-perf-pg", "sql");
  const mongoProject = createProject(auth.access_token, "k6-query-perf-mongo", "nosql");

  let pgReady = null, mongoReady = null;
  if (pgProject) pgReady = waitForProjectReady(auth.access_token, pgProject.id);
  if (mongoProject) mongoReady = waitForProjectReady(auth.access_token, mongoProject.id);

  // Seed PG schema + data
  if (pgReady) {
    const ddl = [
      `CREATE TABLE IF NOT EXISTS k6_users (
        id SERIAL PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT NOW()
      )`,
      `CREATE TABLE IF NOT EXISTS k6_orders (
        id SERIAL PRIMARY KEY,
        user_id INT REFERENCES k6_users(id),
        amount NUMERIC(10,2) NOT NULL,
        status TEXT DEFAULT 'pending',
        created_at TIMESTAMPTZ DEFAULT NOW()
      )`,
    ];
    ddl.forEach((q) => execSQL(auth.access_token, pgProject.id, q));

    // Seed 100 users in a single bulk insert
    let userVals = [];
    for (let i = 1; i <= 100; i++) {
      userVals.push(`('User ${i}', 'user${i}@k6.test')`);
    }
    execSQL(auth.access_token, pgProject.id,
      `INSERT INTO k6_users (name, email) VALUES ${userVals.join(',')} ON CONFLICT DO NOTHING;`
    );

    // Seed 500 orders in a single bulk insert
    let orderVals = [];
    for (let i = 1; i <= 500; i++) {
      orderVals.push(`(${(i % 100) + 1}, ${(Math.random() * 1000).toFixed(2)}, '${["pending","completed","shipped"][i%3]}')`);
    }
    execSQL(auth.access_token, pgProject.id,
      `INSERT INTO k6_orders (user_id, amount, status) VALUES ${orderVals.join(',')};`
    );
  }

  // Seed Mongo collection
  if (mongoReady) {
    const docs = [];
    for (let i = 0; i < 100; i++) {
      docs.push({
        name: `MongoUser ${i}`,
        email: `muser${i}@k6.test`,
        score: Math.floor(Math.random() * 1000),
        tags: ["k6", "load-test"],
      });
    }
    insertMongoDocs(auth.access_token, mongoProject.id, "k6_users", docs);
  }

  return {
    token:          auth.access_token,
    pgProjectId:    pgProject ? pgProject.id : null,
    mongoProjectId: mongoProject ? mongoProject.id : null,
    pgReady:        !!pgReady,
    mongoReady:     !!mongoReady,
  };
}

// ── Default VU (80/20 read/write mix) ───────────────────────────────────────
export default function (data) {
  const roll = Math.random();

  if (roll < 0.4 && data.pgReady) {
    // 40%: PG reads
    pgReadWorkload(data);
  } else if (roll < 0.5 && data.pgReady) {
    // 10%: PG writes
    pgWriteWorkload(data);
  } else if (roll < 0.6 && data.pgReady) {
    // 10%: PG JOINs
    pgJoinWorkload(data);
  } else if (roll < 0.9 && data.mongoReady) {
    // 30%: Mongo reads
    mongoReadWorkload(data);
  } else if (data.mongoReady) {
    // 10%: Mongo writes
    mongoWriteWorkload(data);
  } else if (data.pgReady) {
    pgReadWorkload(data);
  }

  sleep(0.05);
}

// ── PostgreSQL Workloads ────────────────────────────────────────────────────

function pgReadWorkload(data) {
  group("pg_simple_select", () => {
    const start = Date.now();
    const res = execSQL(data.token, data.pgProjectId,
      `SELECT id, name, email FROM k6_users ORDER BY RANDOM() LIMIT 10`
    );
    pgReadLatency.add(Date.now() - start);
    const ok = check(res, { "pg read 200": (r) => r.status === 200 });
    querySuccessRate.add(ok);
    if (!ok) queryErrors.add(1, { type: "pg_read" });
  });
}

function pgWriteWorkload(data) {
  group("pg_insert_update", () => {
    const start = Date.now();
    const vuId = __VU;
    const iter = __ITER;
    const res = execSQL(data.token, data.pgProjectId,
      `INSERT INTO k6_orders (user_id, amount, status) VALUES (${(iter % 100) + 1}, ${(Math.random()*500).toFixed(2)}, 'pending') RETURNING id`
    );
    pgWriteLatency.add(Date.now() - start);
    const ok = check(res, { "pg write 200": (r) => r.status === 200 });
    querySuccessRate.add(ok);
    if (!ok) queryErrors.add(1, { type: "pg_write" });
  });
}

function pgJoinWorkload(data) {
  group("pg_join_query", () => {
    const start = Date.now();
    const res = execSQL(data.token, data.pgProjectId,
      `SELECT u.name, COUNT(o.id) AS order_count, SUM(o.amount) AS total_spent
       FROM k6_users u
       JOIN k6_orders o ON o.user_id = u.id
       GROUP BY u.id, u.name
       ORDER BY total_spent DESC
       LIMIT 20`
    );
    pgJoinLatency.add(Date.now() - start);
    const ok = check(res, { "pg join 200": (r) => r.status === 200 });
    querySuccessRate.add(ok);
    if (!ok) queryErrors.add(1, { type: "pg_join" });
  });
}

// ── MongoDB Workloads ───────────────────────────────────────────────────────

function mongoReadWorkload(data) {
  group("mongo_read", () => {
    const start = Date.now();
    const res = queryMongoDocs(data.token, data.mongoProjectId, "k6_users",
      { score: { $gt: Math.floor(Math.random() * 500) } }, 20, 1
    );
    mongoReadLatency.add(Date.now() - start);
    const ok = check(res, { "mongo read OK": (r) => r.status === 200 });
    querySuccessRate.add(ok);
    if (!ok) queryErrors.add(1, { type: "mongo_read" });
  });
}

function mongoWriteWorkload(data) {
  group("mongo_write", () => {
    const start = Date.now();
    const docs = [{
      name: `VU${__VU}_Iter${__ITER}`,
      email: `vu${__VU}_iter${__ITER}@k6.test`,
      score: Math.floor(Math.random() * 1000),
      tags: ["k6", "write-test"],
    }];
    const res = insertMongoDocs(data.token, data.mongoProjectId, "k6_users", docs);
    mongoWriteLatency.add(Date.now() - start);
    const ok = check(res, { "mongo write OK": (r) => r.status === 200 || r.status === 201 });
    querySuccessRate.add(ok);
    if (!ok) queryErrors.add(1, { type: "mongo_write" });
  });
}

// ── Teardown ────────────────────────────────────────────────────────────────
export function teardown(data) {
  if (data.pgProjectId)    deleteProject(data.token, data.pgProjectId);
  if (data.mongoProjectId) deleteProject(data.token, data.mongoProjectId);
}
