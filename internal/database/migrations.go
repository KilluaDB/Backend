package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunMigrations(pool *pgxpool.Pool) error {
	ctx := context.Background()

	migrations := []string{
		createEnumTypes,
		createUsersTable,
		addRolesColumnToUsers,
		addSoftDeleteToUsers,
		createSessionsTable,
		createProjectsTable,
		createDatabaseInstancesTable,
		createDatabaseCredentialsTable,
		createAPIKeysTable,
		createQueryHistoryTable,
		fixQueryHistoryForeignKey,
		createUsageMetricsTable,
		preventHardDeleteUsers,
	}

	for i, migration := range migrations {
		log.Printf("Running migration %d/%d", i+1, len(migrations))
		if _, err := pool.Exec(ctx, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	log.Println("All migrations completed successfully")
	return nil
}

const createEnumTypes = `
-- Create ENUM types if they don't exist
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
`

const createUsersTable = `
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
`

const addRolesColumnToUsers = `
-- Add roles column to users table if it doesn't exist
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns 
    WHERE table_name = 'users' AND column_name = 'role'
  ) THEN
    ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
    CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
  END IF;
END$$;
`

const addSoftDeleteToUsers = `
-- Add soft-delete support to users table
DO $$
BEGIN
  -- Add deleted_at column if it doesn't exist
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns 
    WHERE table_name = 'users' AND column_name = 'deleted_at'
  ) THEN
    ALTER TABLE users ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE;
    CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);
  END IF;

  -- Add status column if it doesn't exist
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns 
    WHERE table_name = 'users' AND column_name = 'status'
  ) THEN
    ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
  END IF;
END$$;
`

const createSessionsTable = `
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token TEXT NOT NULL,
    is_revoked BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_refresh_token ON sessions(refresh_token);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
`

const createProjectsTable = `
CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  db_type db_type_t NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_projects_user_id ON projects(user_id);
CREATE INDEX IF NOT EXISTS idx_projects_db_type ON projects(db_type);
`

const createDatabaseInstancesTable = `
CREATE TABLE IF NOT EXISTS database_instances (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  cpu_cores INT,
  ram_mb INT,
  storage_gb INT,
  status instance_status_t NOT NULL DEFAULT 'creating',
  endpoint TEXT,
  port INT,
  container_id TEXT,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_database_instances_project_id ON database_instances(project_id);
CREATE INDEX IF NOT EXISTS idx_database_instances_status ON database_instances(status);
CREATE INDEX IF NOT EXISTS idx_database_instances_container_id ON database_instances(container_id);
`

const createDatabaseCredentialsTable = `
CREATE TABLE IF NOT EXISTS database_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  password_encrypted TEXT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_database_credentials_db_instance_id ON database_credentials(db_instance_id);
CREATE INDEX IF NOT EXISTS idx_database_credentials_username ON database_credentials(username);
`

const createAPIKeysTable = `
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
`

const createQueryHistoryTable = `
CREATE TABLE IF NOT EXISTS query_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  db_instance_id UUID NOT NULL REFERENCES database_instances(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
  query_text TEXT NOT NULL,
  executed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  success BOOLEAN,
  execution_time_ms INT
);

CREATE INDEX IF NOT EXISTS idx_query_history_db_instance_id ON query_history(db_instance_id);
CREATE INDEX IF NOT EXISTS idx_query_history_user_id ON query_history(user_id);
CREATE INDEX IF NOT EXISTS idx_query_history_executed_at ON query_history(executed_at);
`

const fixQueryHistoryForeignKey = `
-- Fix query_history foreign key to use RESTRICT instead of SET NULL
DO $$
BEGIN
  -- Drop existing constraint if it exists
  IF EXISTS (
    SELECT 1 FROM information_schema.table_constraints 
    WHERE constraint_name = 'query_history_user_id_fkey' 
    AND table_name = 'query_history'
  ) THEN
    ALTER TABLE query_history DROP CONSTRAINT query_history_user_id_fkey;
  END IF;

  -- Ensure user_id is NOT NULL
  ALTER TABLE query_history ALTER COLUMN user_id SET NOT NULL;

  -- Add correct foreign key with RESTRICT
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.table_constraints 
    WHERE constraint_name = 'query_history_user_fk' 
    AND table_name = 'query_history'
  ) THEN
    ALTER TABLE query_history
    ADD CONSTRAINT query_history_user_fk
    FOREIGN KEY (user_id)
    REFERENCES users(id)
    ON DELETE RESTRICT;
  END IF;
END$$;
`

const preventHardDeleteUsers = `
-- Prevent hard delete of users (enforce soft-delete only)
-- Create or replace function (safe to run multiple times)
CREATE OR REPLACE FUNCTION prevent_hard_delete_users()
RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'Hard delete of users is not allowed. Use soft-delete instead.';
END;
$$ LANGUAGE plpgsql;

-- Drop trigger if exists and recreate (safe to run multiple times)
DROP TRIGGER IF EXISTS no_user_hard_delete ON users;

CREATE TRIGGER no_user_hard_delete
BEFORE DELETE ON users
FOR EACH ROW
EXECUTE FUNCTION prevent_hard_delete_users();
`

const createUsageMetricsTable = `
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
`
