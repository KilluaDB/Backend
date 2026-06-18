// k6-tests/scenarios/05-provisioning-latency.js — PROVISIONING LATENCY
// Measures time from creating a new DB project to it being ready to accept connections.
import { check, sleep } from "k6";
import { Trend, Counter, Rate } from "k6/metrics";
import http from "k6/http";
import { BASE_URL, TEST_EMAIL, TEST_PASSWORD, SLO } from "../lib/config.js";
import { registerUser, authHeaders, deleteProject, healthCheck } from "../lib/helpers.js";


const pgProvisionTime = new Trend("pg_provision_time_seconds", true);
const mongoProvisionTime = new Trend("mongo_provision_time_seconds", true);
const provisionErrors = new Counter("provision_errors_total");
const provisionSuccessRate = new Rate("provision_success_rate");


export const options = { 
   scenarios: { 
      provision_dbs: { 
         executor: "per-vu-iterations", 
         exec: "runBoth",
         vus: 8, // Increased concurrent users
         iterations: 5, // Increased databases provisioned per user
         maxDuration: "1h" 
      } 
   }, 
   thresholds: { 
      pg_provision_time_seconds: [`max<${SLO.provisioning_time_max}`], 
      mongo_provision_time_seconds: [`max<${SLO.provisioning_time_max}`], 
      provision_success_rate: ["rate>0.9"] 
   }, 
   tags: { 
      testSuite: "provisioning_latency" 
   } 
};


export function setup() {
   const auth = registerUser(TEST_EMAIL, TEST_PASSWORD);
   if (!auth) throw new Error("auth failed");
   return { token: auth.access_token };
}

export function runBoth(d) {
   // 1. Run postgres provisioning first and wait until it completely finishes.
   provisionTest(d, "sql", pgProvisionTime);
   
   // 2. Wait briefly before starting the next database to let the cluster breathe
   sleep(5);
   
   // 3. Then run mongodb provisioning sequentially. No more rigid startTimes!
   provisionTest(d, "nosql", mongoProvisionTime);
}

function provisionTest(d, dbType, metricTrend) {
   const name = `k6-prov-${dbType}-vu${__VU}-i${__ITER}`;
   const payload = JSON.stringify({ name, db_type: dbType, password: "K6LoadTest!2026", resource_tier: "free" });
   const start = Date.now();
   const createRes = http.post(`${BASE_URL}/api/v1/projects`, payload, { headers: authHeaders(d.token), tags: { name: "provision_create", db_type: dbType }, timeout: "30s" });
   if (!check(createRes, { "create 201": (r) => r.status === 201 })) {
      provisionErrors.add(1);
      provisionSuccessRate.add(false);
      return;

   } const projectId = createRes.json().data.project.id;

   // Poll until running
   let ready = false;
   const deadline = Date.now() + SLO.provisioning_time_max * 1000;
   while (Date.now() < deadline) {
      sleep(3);
      const res = http.get(`${BASE_URL}/api/v1/projects/${projectId}`, { headers: authHeaders(d.token), tags: { name: "provision_poll", db_type: dbType } });
      if (res.status === 200) {
         const st = res.json().data.status;
         if (st === "running") {
            ready = true;
            break;

         } if (st === "failed") {
            break;

         }
      }
   }
   const elapsed = (Date.now() - start) / 1000;
   metricTrend.add(elapsed);
   provisionSuccessRate.add(ready);
   if (!ready) provisionErrors.add(1);

   // Verify connectivity
   if (ready) {
      const accessRes = http.get(`${BASE_URL}/api/v1/projects/${projectId}/access`, { headers: authHeaders(d.token), tags: { name: "provision_access", db_type: dbType } });
      check(accessRes, { "access 200": (r) => r.status === 200 });
   }
   // Cleanup
   deleteProject(d.token, projectId);
   sleep(2);
}

export function teardown(d) { }
