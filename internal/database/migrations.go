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
		createProjectsTable,
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
`

const createUsersTable = `
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'user',
  status TEXT NOT NULL DEFAULT 'active',
  deleted_at TIMESTAMP WITH TIME ZONE,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  last_login_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);
`

const createProjectsTable = `
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

ALTER TABLE projects ADD COLUMN IF NOT EXISTS resource_tier resource_tier_t NOT NULL DEFAULT 'free';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS status instance_status_t NOT NULL DEFAULT 'creating';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS runtime_created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW();
ALTER TABLE projects ADD COLUMN IF NOT EXISTS runtime_updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_projects_user_id ON projects(user_id);
CREATE INDEX IF NOT EXISTS idx_projects_db_type ON projects(db_type);
CREATE INDEX IF NOT EXISTS idx_projects_resource_tier ON projects(resource_tier);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
`