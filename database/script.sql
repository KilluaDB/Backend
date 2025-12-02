-- CREATE TYPE IF NOT EXISTS db_type_t AS ENUM ('postgres', 'mongodb');

-- CREATE TYPE instance_status_t AS ENUM ('creating', 'running', 'failed', 'paused', 'deleted');

-- CREATE TYPE backup_type_t AS ENUM ('auto', 'manual');

-- CREATE TYPE restore_status_t AS ENUM ('pending', 'running', 'success', 'failed');

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'db_type_t') THEN
    CREATE TYPE db_type_t AS ENUM ('postgres', 'mongodb');
  END IF;
END$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'instance_status_t') THEN
        CREATE TYPE instance_status_t AS ENUM ('creating', 'running', 'failed', 'paused', 'deleted');
    END IF;
END$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'backup_type_t') THEN
        CREATE TYPE backup_type_t AS ENUM ('auto', 'manual');
    END IF;
END$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'restore_status_t') THEN
        CREATE TYPE restore_status_t AS ENUM ('pending', 'running', 'success', 'failed');
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  db_type db_type_t NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS database_instances (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
--   version TEXT,                -- engine version (eg '16.1' or '7.0')
  cpu_cores INT,
  ram_mb INT,
  storage_gb INT,
  status instance_status_t NOT NULL DEFAULT 'creating',
  endpoint TEXT,
  port INT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS database_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  password_encrypted TEXT NOT NULL,  -- store encrypted/hashed secrets (NOT plain)
--   is_readonly BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key_hash TEXT NOT NULL,        -- store hash of key, never the plain key
  description TEXT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMP WITH TIME ZONE,
  revoked BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS query_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
  query_text TEXT NOT NULL,
  executed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  success BOOLEAN,
  execution_time_ms INT
);

CREATE TABLE IF NOT EXISTS backups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  backup_type backup_type_t NOT NULL,
  path TEXT NOT NULL,            -- object store path or URI
  size_mb BIGINT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS restore_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  backup_id UUID NOT NULL REFERENCES backups(id) ON DELETE CASCADE,
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  status restore_status_t NOT NULL DEFAULT 'pending',
  started_at TIMESTAMP WITH TIME ZONE,
  finished_at TIMESTAMP WITH TIME ZONE,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS usage_metrics (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  cpu_percent REAL,
  ram_percent REAL,
  storage_used_gb REAL,
  bandwidth_in_gb REAL,
  bandwidth_out_gb REAL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
  action TEXT NOT NULL,       -- e.g., 'created db', 'deleted backup', 'ran query'
--   metadata JSONB,             -- structured extra info (IDs, diff, etc.)
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- CREATE TABLE firewall_rules (
--   id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
--   db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
--   cidr TEXT NOT NULL,        -- e.g., '203.0.113.0/24'
--   description TEXT,
--   created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
-- );

