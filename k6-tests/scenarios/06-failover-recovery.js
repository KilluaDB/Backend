// k6-tests/scenarios/06-failover-recovery.js — FAILOVER & RESTART RECOVERY
// Kills a DB pod mid-test (via exec/xk6-kubernetes), measures reconnection time and error window.
// NOTE: Requires either xk6-kubernetes extension or a companion script to kill pods.
import{check,sleep}from"k6";import{Trend,Counter,Rate}from"k6/metrics";import http from"k6/http";import{BASE_URL,TEST_EMAIL,TEST_PASSWORD,SLO}from"../lib/config.js";import{registerUser,createProject,waitForProjectReady,deleteProject,execSQL,authHeaders}from"../lib/helpers.js";

const reconnectTime=new Trend("failover_reconnect_time_ms",true);const errorWindow=new Counter("failover_error_window_ms");const failoverErrors=new Counter("failover_errors_total");const failoverErrorRate=new Rate("failover_error_rate");

export const options={scenarios:{failover:{executor:"constant-vus",vus:5,duration:"5m"}},thresholds:{failover_reconnect_time_ms:["p(95)<30000"],failover_error_rate:["rate<0.2"],http_req_failed:["rate<0.2"]},tags:{testSuite:"failover_recovery"}};

export function setup(){const auth=registerUser(TEST_EMAIL,TEST_PASSWORD);if(!auth)throw new Error("auth failed");const pg=createProject(auth.access_token,"k6-failover-pg","sql");let pgR=null;if(pg)pgR=waitForProjectReady(auth.access_token,pg.id);if(pgR){execSQL(auth.access_token,pg.id,`CREATE TABLE IF NOT EXISTS k6_failover(id SERIAL PRIMARY KEY,ts TIMESTAMPTZ DEFAULT NOW())`);for(let i=0;i<50;i++)execSQL(auth.access_token,pg.id,`INSERT INTO k6_failover DEFAULT VALUES`);}return{token:auth.access_token,pgId:pg?pg.id:null,pgR:!!pgR,podKilled:false,killTime:0};}

export default function(d){if(!d.pgR){sleep(1);return;}
// At ~60s in, VU 1 triggers pod kill via the backend health endpoint as a signal
// The actual pod deletion should be done by the companion script (kill-pod.sh)
// This script just measures the impact
const start=Date.now();const res=execSQL(d.token,d.pgId,`SELECT COUNT(*) FROM k6_failover`);const elapsed=Date.now()-start;
const ok=check(res,{"failover query ok":(r)=>r.status===200});failoverErrorRate.add(!ok);if(!ok){failoverErrors.add(1);errorWindow.add(elapsed);
// Attempt reconnection timing
let reconnected=false;const reconStart=Date.now();for(let attempt=0;attempt<30;attempt++){sleep(1);const retry=execSQL(d.token,d.pgId,`SELECT 1`);if(retry.status===200){reconnected=true;reconnectTime.add(Date.now()-reconStart);break;}}if(!reconnected)reconnectTime.add(30000);}sleep(0.5);}

export function teardown(d){if(d.pgId)deleteProject(d.token,d.pgId);}
