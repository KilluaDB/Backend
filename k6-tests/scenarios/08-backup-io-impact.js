// k6-tests/scenarios/08-backup-io-impact.js — BACKUP I/O IMPACT
// Triggers a backup (export) while running load queries to measure I/O contention impact.
import{check,sleep}from"k6";import{Trend,Rate,Counter}from"k6/metrics";import http from"k6/http";import{BASE_URL,TEST_EMAIL,TEST_PASSWORD,SLO}from"../lib/config.js";import{registerUser,createProject,waitForProjectReady,deleteProject,execSQL,authHeaders}from"../lib/helpers.js";

const queryDuringBackup=new Trend("query_during_backup_ms",true);const queryNoBackup=new Trend("query_no_backup_ms",true);const backupDuration=new Trend("backup_duration_seconds",true);const backupErrorRate=new Rate("backup_error_rate");

export const options={scenarios:{baseline_load:{executor:"constant-vus",vus:10,duration:"3m",tags:{phase:"baseline"}},backup_load:{executor:"constant-vus",vus:10,duration:"3m",startTime:"3m",tags:{phase:"during_backup"}},post_backup:{executor:"constant-vus",vus:10,duration:"2m",startTime:"6m",tags:{phase:"post_backup"}}},thresholds:{query_during_backup_ms:["p(95)<500"],query_no_backup_ms:["p(95)<200"],backup_error_rate:["rate<0.05"],http_req_failed:[`rate<${SLO.http_req_failed_rate}`]},tags:{testSuite:"backup_io_impact"}};

export function setup(){const auth=registerUser(TEST_EMAIL,TEST_PASSWORD);if(!auth)throw new Error("auth failed");const pg=createProject(auth.access_token,"k6-backup-io-pg","sql");let pgR=null;if(pg)pgR=waitForProjectReady(auth.access_token,pg.id);if(pgR){execSQL(auth.access_token,pg.id,`CREATE TABLE IF NOT EXISTS k6_backup_data(id SERIAL PRIMARY KEY,payload TEXT DEFAULT repeat('abcdef',100),ts TIMESTAMPTZ DEFAULT NOW())`);for(let i=0;i<500;i++)execSQL(auth.access_token,pg.id,`INSERT INTO k6_backup_data(payload) VALUES(repeat('data-${i%26}',50))`);}return{token:auth.access_token,pgId:pg?pg.id:null,pgR:!!pgR,backupTriggered:false};}

export default function(d){if(!d.pgR){sleep(1);return;}const phase=__ENV.K6_SCENARIO_NAME||"baseline_load";
// During backup_load phase, VU 1 triggers the export
if(phase==="backup_load"&&__VU===1&&__ITER===0){const bs=Date.now();const bRes=http.get(`${BASE_URL}/api/v1/projects/${d.pgId}/export?format=sql`,{headers:authHeaders(d.token),tags:{name:"trigger_backup"},timeout:"120s"});backupDuration.add((Date.now()-bs)/1000);check(bRes,{"backup ok":(r)=>r.status===200});return;}
// Normal query load
const s=Date.now();const r=execSQL(d.token,d.pgId,`SELECT id,LEFT(payload,20) FROM k6_backup_data ORDER BY RANDOM() LIMIT 20`);const e=Date.now()-s;if(phase==="backup_load"){queryDuringBackup.add(e);}else{queryNoBackup.add(e);}const ok=check(r,{"query ok":(r)=>r.status===200});backupErrorRate.add(!ok);sleep(0.1);}

export function teardown(d){if(d.pgId)deleteProject(d.token,d.pgId);}
