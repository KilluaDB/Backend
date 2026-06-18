// k6-tests/scenarios/04-spike-test.js — SPIKE TEST
// Sudden burst from 0 to peak VUs, measuring recovery and error rate.
import { check, sleep } from "k6";
import { Trend, Counter, Rate } from "k6/metrics";
import { TEST_EMAIL, TEST_PASSWORD, SLO } from "../lib/config.js";
import { registerUser, createProject, waitForProjectReady, deleteProject, execSQL, queryMongoDocs } from "../lib/helpers.js";


const spikeLatency = new Trend("spike_latency_ms", true);
const spikeErrors = new Counter("spike_errors_total");
const spikeErrorRate = new Rate("spike_error_rate");
const recoveryTime = new Trend("spike_recovery_time_ms", true);


export const options = { scenarios: { spike: { executor: "ramping-vus", startVUs: 0, stages: [{ duration: "10s", target: 0 }, { duration: "5s", target: 100 }, { duration: "1m", target: 100 }, { duration: "10s", target: 0 }, { duration: "30s", target: 0 }, { duration: "5s", target: 80 }, { duration: "1m", target: 80 }, { duration: "10s", target: 0 }, { duration: "30s", target: 0 }] } }, thresholds: { spike_latency_ms: ["p(95)<500", "p(99)<1000"], spike_error_rate: ["rate<0.05"], http_req_duration: [`p(95)<500`], http_req_failed: ["rate<0.05"] }, tags: { testSuite: "spike_test" } };


export function setup() {
   const auth = registerUser(TEST_EMAIL, TEST_PASSWORD);
   if (!auth) throw new Error("auth failed");
   const pg = createProject(auth.access_token, "k6-spike-pg", "sql");
   const mg = createProject(auth.access_token, "k6-spike-mongo", "nosql");
   let pgR = null, mgR = null;
   if (pg) pgR = waitForProjectReady(auth.access_token, pg.id);
   if (mg) mgR = waitForProjectReady(auth.access_token, mg.id);
   if (pgR) {
      execSQL(auth.access_token, pg.id, `CREATE TABLE IF NOT EXISTS k6_spike(id SERIAL PRIMARY KEY,data TEXT DEFAULT 'spike',ts TIMESTAMPTZ DEFAULT NOW())`);
      for (let i = 0;
         i < 100;
         i++)execSQL(auth.access_token, pg.id, `INSERT INTO k6_spike(data) VALUES('seed-${i}')`);
   } return { token: auth.access_token, pgId: pg ? pg.id : null, mgId: mg ? mg.id : null, pgR: !!pgR, mgR: !!mgR };
}

export default function (d) {
   const roll = Math.random();
   let start, res, ok;
   if (roll < 0.6 && d.pgR) {
      start = Date.now();
      res = execSQL(d.token, d.pgId, `SELECT * FROM k6_spike ORDER BY RANDOM() LIMIT 10`);
      const e = Date.now() - start;
      spikeLatency.add(e);
      ok = check(res, { "spike pg ok": (r) => r.status === 200 });
      spikeErrorRate.add(!ok);
      if (!ok) spikeErrors.add(1);
   } else if (d.mgR) {
      start = Date.now();
      res = queryMongoDocs(d.token, d.mgId, "k6_spike", {}, 10, 1);
      const e = Date.now() - start;
      spikeLatency.add(e);
      ok = check(res, { "spike mg ok": (r) => r.status === 200 || r.status === 404 });
      spikeErrorRate.add(!ok);
      if (!ok) spikeErrors.add(1);
   } else if (d.pgR) {
      start = Date.now();
      res = execSQL(d.token, d.pgId, `SELECT 1`);
      spikeLatency.add(Date.now() - start);
      ok = check(res, { "spike fallback ok": (r) => r.status === 200 });
      spikeErrorRate.add(!ok);
   } sleep(0.05);
}

export function teardown(d) {
   if (d.pgId) deleteProject(d.token, d.pgId);
   if (d.mgId) deleteProject(d.token, d.mgId);

}
