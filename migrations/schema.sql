-- Database Schema for Database-as-a-Service Platform

-- Create ENUM types if they don't exist
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'db_type_t') THEN
    CREATE TYPE db_type_t AS ENUM ('postgresql', 'mongodb');
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
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'resource_tier_t') THEN
        CREATE TYPE resource_tier_t AS ENUM ('free', 'basic', 'premium');
    END IF;
END$$;


-- Users table
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);




-- Projects table
CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  db_type db_type_t NOT NULL,
  resource_tier resource_tier_t NOT NULL DEFAULT 'free',
  status instance_status_t NOT NULL DEFAULT 'creating',
  runtime_created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  runtime_updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_projects_user_id ON projects(user_id);
CREATE INDEX IF NOT EXISTS idx_projects_db_type ON projects(db_type);
CREATE INDEX IF NOT EXISTS idx_projects_resource_tier ON projects(resource_tier);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);


-- Database Instances table (K8s resource discovered by project_id via cluster name convention)
CREATE TABLE IF NOT EXISTS database_instances (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  cpu_cores INT,
  ram_mb INT,
  storage_gb INT,
  status instance_status_t NOT NULL DEFAULT 'creating',
  port INT,
  host TEXT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_database_instances_project_id ON database_instances(project_id);
CREATE INDEX IF NOT EXISTS idx_database_instances_status ON database_instances(status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_database_instances_project_id ON database_instances(project_id);

-- Database Credentials table
CREATE TABLE IF NOT EXISTS database_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  password_encrypted TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_database_credentials_db_instance_id ON database_credentials(db_instance_id);
CREATE INDEX IF NOT EXISTS idx_database_credentials_username ON database_credentials(username);
CREATE UNIQUE INDEX IF NOT EXISTS idx_database_credentials_instance_username ON database_credentials(db_instance_id, username);


-- API Keys table
CREATE TABLE IF NOT EXISTS api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key_hash TEXT NOT NULL,
  description TEXT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMP WITH TIME ZONE,
  revoked BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_revoked ON api_keys(revoked);
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys(expires_at);


CREATE TABLE IF NOT EXISTS mongo_query_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
  query_text TEXT NOT NULL,
  executed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  success BOOLEAN,
  execution_time_ms INT
);

CREATE INDEX IF NOT EXISTS idx_mongo_query_history_db_instance_id ON mongo_query_history(db_instance_id);
CREATE INDEX IF NOT EXISTS idx_mongo_query_history_user_id ON mongo_query_history(user_id);
CREATE INDEX IF NOT EXISTS idx_mongo_query_history_executed_at ON mongo_query_history(executed_at);


-- Usage Metrics table
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

CREATE INDEX IF NOT EXISTS idx_usage_metrics_db_instance_id ON usage_metrics(db_instance_id);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_timestamp ON usage_metrics(timestamp);
