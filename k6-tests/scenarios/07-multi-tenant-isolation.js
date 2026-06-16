// k6-tests/scenarios/07-multi-tenant-isolation.js — MULTI-TENANT ISOLATION
// Runs concurrent VUs against different tenant DB instances to detect noisy neighbor effects.
import{check,sleep,group}from"k6";import{Trend,Rate}from"k6/metrics";import{BASE_URL,TEST_EMAIL,TEST_PASSWORD,TENANT_B_EMAIL,TENANT_B_PASSWORD,SLO}from"../lib/config.js";import{registerUser,createProject,waitForProjectReady,deleteProject,execSQL,queryMongoDocs,insertMongoDocs}from"../lib/helpers.js";

const tenantALatency=new Trend("tenant_a_latency_ms",true);const tenantBLatency=new Trend("tenant_b_latency_ms",true);const tenantAErrors=new Rate("tenant_a_error_rate");const tenantBErrors=new Rate("tenant_b_error_rate");

export const options={scenarios:{tenant_a_load:{executor:"constant-vus",vus:15,duration:"5m",tags:{tenant:"A"},env:{TENANT:"A"}},tenant_b_load:{executor:"constant-vus",vus:15,duration:"5m",tags:{tenant:"B"},env:{TENANT:"B"}}},thresholds:{tenant_a_latency_ms:["p(95)<200","p(99)<500"],tenant_b_latency_ms:["p(95)<200","p(99)<500"],tenant_a_error_rate:["rate<0.01"],tenant_b_error_rate:["rate<0.01"],http_req_duration:[`p(95)<${SLO.http_req_duration_p95}`]},tags:{testSuite:"multi_tenant_isolation"}};

export function setup(){const authA=registerUser(TEST_EMAIL,TEST_PASSWORD);const authB=registerUser(TENANT_B_EMAIL,TENANT_B_PASSWORD);if(!authA||!authB)throw new Error("auth failed");
const pgA=createProject(authA.access_token,"k6-tenant-a-pg","sql");const pgB=createProject(authB.access_token,"k6-tenant-b-pg","sql");
let pgAR=null,pgBR=null;if(pgA)pgAR=waitForProjectReady(authA.access_token,pgA.id);if(pgB)pgBR=waitForProjectReady(authB.access_token,pgB.id);
if(pgAR){execSQL(authA.access_token,pgA.id,`CREATE TABLE IF NOT EXISTS k6_tenant(id SERIAL PRIMARY KEY,data TEXT,ts TIMESTAMPTZ DEFAULT NOW())`);for(let i=0;i<100;i++)execSQL(authA.access_token,pgA.id,`INSERT INTO k6_tenant(data) VALUES('A-${i}')`);}
if(pgBR){execSQL(authB.access_token,pgB.id,`CREATE TABLE IF NOT EXISTS k6_tenant(id SERIAL PRIMARY KEY,data TEXT,ts TIMESTAMPTZ DEFAULT NOW())`);for(let i=0;i<100;i++)execSQL(authB.access_token,pgB.id,`INSERT INTO k6_tenant(data) VALUES('B-${i}')`);}
return{tokenA:authA.access_token,tokenB:authB.access_token,pgAId:pgA?pgA.id:null,pgBId:pgB?pgB.id:null,pgAR:!!pgAR,pgBR:!!pgBR};}

export default function(d){const tenant=__ENV.TENANT||"A";if(tenant==="A"&&d.pgAR){const s=Date.now();const r=execSQL(d.tokenA,d.pgAId,Math.random()<0.8?`SELECT * FROM k6_tenant ORDER BY RANDOM() LIMIT 20`:`INSERT INTO k6_tenant(data) VALUES('A-vu${__VU}-${__ITER}') RETURNING id`);tenantALatency.add(Date.now()-s);tenantAErrors.add(!check(r,{"A ok":(r)=>r.status===200}));}else if(tenant==="B"&&d.pgBR){const s=Date.now();const r=execSQL(d.tokenB,d.pgBId,Math.random()<0.8?`SELECT * FROM k6_tenant ORDER BY RANDOM() LIMIT 20`:`INSERT INTO k6_tenant(data) VALUES('B-vu${__VU}-${__ITER}') RETURNING id`);tenantBLatency.add(Date.now()-s);tenantBErrors.add(!check(r,{"B ok":(r)=>r.status===200}));}sleep(0.1);}

export function teardown(d){if(d.pgAId)deleteProject(d.tokenA,d.pgAId);if(d.pgBId)deleteProject(d.tokenB,d.pgBId);}
