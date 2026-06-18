// k6-tests/scenarios/09-resource-ceiling.js — RESOURCE CEILING (k3s single-node)
// Ramps load until the cluster starts dropping requests. Uses strict p95/error thresholds.
import { check, sleep } from "k6";
import { Trend, Rate, Counter } from "k6/metrics";
import { TEST_EMAIL, TEST_PASSWORD, SLO } from "../lib/config.js";
import { registerUser, createProject, waitForProjectReady, deleteProject, execSQL, healthCheck } from "../lib/helpers.js";


const ceilingLatency = new Trend("ceiling_latency_ms", true);
const ceilingErrorRate = new Rate("ceiling_error_rate");
const droppedRequests = new Counter("dropped_requests_total");


export const options = { scenarios: { ramp_to_ceiling: { executor: "ramping-vus", startVUs: 1, stages: [{ duration: "30s", target: 10 }, { duration: "1m", target: 30 }, { duration: "1m", target: 60 }, { duration: "1m", target: 100 }, { duration: "1m", target: 150 }, { duration: "1m", target: 200 }, { duration: "30s", target: 0 }] } }, thresholds: { ceiling_latency_ms: [`p(95)<${SLO.http_req_duration_p95}`], ceiling_error_rate: [`rate<${SLO.http_req_failed_rate}`], http_req_duration: [`p(95)<${SLO.http_req_duration_p95}`], http_req_failed: [`rate<${SLO.http_req_failed_rate}`] }, tags: { testSuite: "resource_ceiling" } };


export function setup() {
   const auth = registerUser(TEST_EMAIL, TEST_PASSWORD);
   if (!auth) throw new Error("auth failed");
   const pg = createProject(auth.access_token, "k6-ceiling-pg", "sql");
   let pgR = null;
   if (pg) pgR = waitForProjectReady(auth.access_token, pg.id);
   if (pgR) {
      execSQL(auth.access_token, pg.id, `CREATE TABLE IF NOT EXISTS k6_ceiling(id SERIAL PRIMARY KEY,v INT DEFAULT 0)`);
      for (let i = 0;
         i < 200;
         i++)execSQL(auth.access_token, pg.id, `INSERT INTO k6_ceiling(v) VALUES(${i})`);
   } return { token: auth.access_token, pgId: pg ? pg.id : null, pgR: !!pgR };
}

export default function (d) {
   if (!d.pgR) {
      healthCheck();
      sleep(0.1);
      return;

   } const s = Date.now();
   const r = execSQL(d.token, d.pgId, Math.random() < 0.7 ? `SELECT * FROM k6_ceiling WHERE v>${Math.floor(Math.random() * 200)} LIMIT 20` : `INSERT INTO k6_ceiling(v) VALUES(${Math.floor(Math.random() * 10000)}) RETURNING id`);
   const e = Date.now() - s;
   ceilingLatency.add(e);
   const ok = check(r, { "ceiling ok": (r) => r.status === 200 });
   ceilingErrorRate.add(!ok);
   if (!ok) droppedRequests.add(1);
   sleep(0.02);
}

export function teardown(d) {
   if (d.pgId) deleteProject(d.token, d.pgId);

}
