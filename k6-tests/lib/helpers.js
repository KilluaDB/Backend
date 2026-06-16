// ─────────────────────────────────────────────────────────────────────────────
// k6-tests/lib/helpers.js — Shared utility functions for KilluaDB k6 tests
// ─────────────────────────────────────────────────────────────────────────────

import http from "k6/http";
import { check, sleep } from "k6";
import { BASE_URL } from "./config.js";

// ── Authentication ──────────────────────────────────────────────────────────

/**
 * Register a test user. Returns { user_id, access_token } or null on failure.
 */
export function registerUser(email, password) {
  const res = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, password }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "auth_register" } }
  );
  if (res.status === 201 || res.status === 409) {
    // 409 = already exists → login instead
    if (res.status === 409) return loginUser(email, password);
    const body = res.json();
    return body.data;
  }
  return null;
}

/**
 * Login and return { user_id, access_token }.
 */
export function loginUser(email, password) {
  const res = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ email, password }),
    { headers: { "Content-Type": "application/json" }, tags: { name: "auth_login" } }
  );
  check(res, { "login 200": (r) => r.status === 200 });
  if (res.status === 200) {
    return res.json().data;
  }
  return null;
}

/**
 * Build standard auth headers.
 */
export function authHeaders(token) {
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  };
}

// ── Project helpers ─────────────────────────────────────────────────────────

/**
 * Create a project (SQL or NoSQL) and return the project object.
 * dbType: "sql" | "nosql"
 */
export function createProject(token, name, dbType, tier = "free") {
  const payload = {
    name,
    db_type: dbType,
    password: "K6LoadTest!2026",
    resource_tier: tier,
  };
  const res = http.post(
    `${BASE_URL}/api/v1/projects`,
    JSON.stringify(payload),
    { headers: authHeaders(token), tags: { name: "create_project" } }
  );
  check(res, { "project created (201)": (r) => r.status === 201 });
  if (res.status === 201) {
    return res.json().data.project;
  }
  return null;
}

/**
 * Poll project until its status == "running" (or timeout).
 * Returns the project object or null.
 */
export function waitForProjectReady(token, projectId, timeoutMs = 300000, pollMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const res = http.get(`${BASE_URL}/api/v1/projects/${projectId}`, {
      headers: authHeaders(token),
      tags: { name: "poll_project_status" },
    });
    if (res.status === 200) {
      const project = res.json().data;
      if (project.status === "running") return project;
      if (project.status === "failed") return null;
    }
    sleep(pollMs / 1000);
  }
  return null;
}

/**
 * Delete a project (cleanup).
 */
export function deleteProject(token, projectId) {
  const res = http.del(`${BASE_URL}/api/v1/projects/${projectId}`, null, {
    headers: authHeaders(token),
    tags: { name: "delete_project" },
  });
  return res.status === 200;
}

/**
 * Get project access/connection info.
 */
export function getProjectAccess(token, projectId) {
  const res = http.get(`${BASE_URL}/api/v1/projects/${projectId}/access`, {
    headers: authHeaders(token),
    tags: { name: "get_project_access" },
  });
  if (res.status === 200) {
    return res.json().data;
  }
  return null;
}

// ── PostgreSQL helpers ──────────────────────────────────────────────────────

/**
 * Execute a raw SQL query against a project's Postgres.
 */
export function execSQL(token, projectId, query) {
  const res = http.post(
    `${BASE_URL}/api/v1/projects/${projectId}/postgres/query/execute`,
    JSON.stringify({ query }),
    { headers: authHeaders(token), tags: { name: "pg_query_exec" } }
  );
  return res;
}

/**
 * List Postgres tables.
 */
export function listPgTables(token, projectId) {
  const res = http.get(
    `${BASE_URL}/api/v1/projects/${projectId}/postgres/tables`,
    { headers: authHeaders(token), tags: { name: "pg_list_tables" } }
  );
  return res;
}

/**
 * Insert a row into a Postgres table.
 */
export function insertPgRow(token, projectId, table, values) {
  const res = http.post(
    `${BASE_URL}/api/v1/projects/${projectId}/postgres/tables/${table}/rows`,
    JSON.stringify({ values }),
    { headers: authHeaders(token), tags: { name: "pg_insert_row" } }
  );
  return res;
}

/**
 * Get rows from a Postgres table.
 */
export function getPgRows(token, projectId, table, limit = 50, offset = 0) {
  const res = http.get(
    `${BASE_URL}/api/v1/projects/${projectId}/postgres/tables/${table}/rows?limit=${limit}&offset=${offset}`,
    { headers: authHeaders(token), tags: { name: "pg_get_rows" } }
  );
  return res;
}

// ── MongoDB helpers ─────────────────────────────────────────────────────────

/**
 * Insert documents into a MongoDB collection.
 */
export function insertMongoDocs(token, projectId, collection, documents) {
  const res = http.post(
    `${BASE_URL}/api/v1/projects/${projectId}/mongodb/collections/${collection}/documents`,
    JSON.stringify({ documents }),
    { headers: authHeaders(token), tags: { name: "mongo_insert_docs" } }
  );
  return res;
}

/**
 * Query MongoDB documents.
 */
export function queryMongoDocs(token, projectId, collection, filter = {}, limit = 20, page = 1) {
  const res = http.post(
    `${BASE_URL}/api/v1/projects/${projectId}/mongodb/collections/${collection}/documents/query`,
    JSON.stringify({ filter, limit, page }),
    { headers: authHeaders(token), tags: { name: "mongo_query_docs" } }
  );
  return res;
}

/**
 * Get documents from a MongoDB collection.
 */
export function getMongoDocs(token, projectId, collection, limit = 20, page = 1) {
  const res = http.get(
    `${BASE_URL}/api/v1/projects/${projectId}/mongodb/collections/${collection}/documents?limit=${limit}&page=${page}`,
    { headers: authHeaders(token), tags: { name: "mongo_get_docs" } }
  );
  return res;
}

/**
 * Update MongoDB documents.
 */
export function updateMongoDocs(token, projectId, collection, filter, update) {
  const res = http.patch(
    `${BASE_URL}/api/v1/projects/${projectId}/mongodb/collections/${collection}/documents`,
    JSON.stringify({ filter, update }),
    { headers: authHeaders(token), tags: { name: "mongo_update_docs" } }
  );
  return res;
}

// ── Dashboard / Metrics helpers ─────────────────────────────────────────────

/**
 * Get Postgres dashboard metrics.
 */
export function getPgDashboardMetrics(token, projectId) {
  const res = http.get(
    `${BASE_URL}/api/v1/projects/${projectId}/postgres/dashboard/metrics`,
    { headers: authHeaders(token), tags: { name: "pg_dashboard_metrics" } }
  );
  return res;
}

/**
 * Get MongoDB dashboard metrics.
 */
export function getMongoDashboardMetrics(token, projectId) {
  const res = http.get(
    `${BASE_URL}/api/v1/projects/${projectId}/mongodb/dashboard/metrics`,
    { headers: authHeaders(token), tags: { name: "mongo_dashboard_metrics" } }
  );
  return res;
}

// ── Generic ─────────────────────────────────────────────────────────────────

/**
 * Health check.
 */
export function healthCheck() {
  const res = http.get(`${BASE_URL}/health`, { tags: { name: "health_check" } });
  check(res, { "health 200": (r) => r.status === 200 });
  return res;
}
