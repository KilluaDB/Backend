# Database Migrations

This directory contains SQL migration scripts for creating the database schema.

## Migration Files

The migrations are numbered sequentially and should be run in order:

1. `001_create_users_table.sql` - Creates the users table
2. `002_create_sessions_table.sql` - Creates the sessions table
3. `003_create_database_connections_table.sql` - Creates the database_connections table
4. `004_create_query_executions_table.sql` - Creates the query_executions table

## Running Migrations

### Option 1: Manual Execution

Run each migration file in order using `psql`:

```bash
# Connect to your database
psql -h localhost -U postgres -d your_app_db

# Run migrations
\i migrations/001_create_users_table.sql
\i migrations/002_create_sessions_table.sql
\i migrations/003_create_database_connections_table.sql
\i migrations/004_create_query_executions_table.sql
```

### Option 2: Using psql from Command Line

```bash
psql -h localhost -U postgres -d your_app_db -f migrations/001_create_users_table.sql
psql -h localhost -U postgres -d your_app_db -f migrations/002_create_sessions_table.sql
psql -h localhost -U postgres -d your_app_db -f migrations/003_create_database_connections_table.sql
psql -h localhost -U postgres -d your_app_db -f migrations/004_create_query_executions_table.sql
```

### Option 3: Run All Migrations at Once

```bash
# From the backend directory
for file in migrations/*.sql; do
    psql -h localhost -U postgres -d your_app_db -f "$file"
done
```

### Option 4: Using GORM AutoMigrate (Current Default)

The application currently uses GORM's AutoMigrate feature which automatically creates tables on startup. This is configured in `internal/server/server.go`:

```go
db.AutoMigrate(
    &models.User{},
    &models.Session{},
    &models.DatabaseConnection{},
    &models.QueryExecution{},
)
```

## Migration Order

**Important:** Migrations must be run in order because of foreign key dependencies:

1. `users` table (no dependencies)
2. `sessions` table (depends on `users`)
3. `database_connections` table (depends on `users`)
4. `query_executions` table (depends on `users` and `database_connections`)

## Table Relationships

```
users (1) ──< (many) sessions
users (1) ──< (many) database_connections
users (1) ──< (many) query_executions
database_connections (1) ──< (many) query_executions
```

## Notes

- All tables use UUID as primary keys
- Foreign keys use `ON DELETE CASCADE` to maintain referential integrity
- Indexes are created for frequently queried columns
- Timestamps use `CURRENT_TIMESTAMP` as default
- The `password` field in `database_connections` should be encrypted in production

## Verifying Migrations

After running migrations, verify tables were created:

```sql
-- List all tables
\dt

-- Check table structure
\d users
\d sessions
\d database_connections
\d query_executions

-- Check indexes
\di
```

## Rolling Back Migrations

To drop all tables (use with caution):

```sql
DROP TABLE IF EXISTS query_executions CASCADE;
DROP TABLE IF EXISTS database_connections CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS users CASCADE;
```

**Warning:** This will delete all data!


