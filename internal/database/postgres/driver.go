package postgres

import (
	"backend/internal/database"
	"backend/internal/repositories"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Driver is a PostgreSQL implementation of database.DatabaseDriver.
// It uses the main metadata pool to resolve per-project instances and
// opens a short-lived pool to the project's database for each operation.
type Driver struct {
	metaPool *pgxpool.Pool
}

var _ database.DatabaseDriver = (*Driver)(nil)

func NewDriver(metaPool *pgxpool.Pool) *Driver {
	return &Driver{metaPool: metaPool}
}

// getProjectPool resolves the running instance for a project and opens
// a connection pool to the project's "app" database.
func (d *Driver) getProjectPool(ctx context.Context, projectID string) (*pgxpool.Pool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}

	instRepo := repositories.NewDatabaseInstanceRepository(d.metaPool)
	credRepo := repositories.NewDatabaseCredentialRepository(d.metaPool)

	inst, err := instRepo.GetConnectableByProjectID(projectUUID)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.Host == nil || inst.Port == nil {
		return nil, errors.New("no connectable PostgreSQL instance for project")
	}

	cred, err := credRepo.GetLatestByInstanceID(inst.ID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("no credentials configured for PostgreSQL instance")
	}

	password, err := utils.DecryptString(cred.PasswordEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt database credentials: %w", err)
	}

	pool, err := database.ConnectToPostgresProject(*inst.Host, *inst.Port, cred.Username, password, "app")
	if err != nil {
		return nil, err
	}

	return pool, nil
}

// identifier validation (very similar to existing table service).
var identifierRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]*$`)

func isValidIdentifier(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	return identifierRE.MatchString(name)
}

func (d *Driver) CreateContainer(ctx context.Context, projectID string, name string) error {
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid container name: %s", name)
	}
	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s" (id SERIAL PRIMARY KEY)`, name)
	_, err = pool.Exec(ctx, query)
	return err
}

func (d *Driver) DeleteContainer(ctx context.Context, projectID string, name string) error {
	if !isValidIdentifier(name) {
		return fmt.Errorf("invalid container name: %s", name)
	}
	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	query := fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, name)
	_, err = pool.Exec(ctx, query)
	return err
}

func (d *Driver) ListContainers(ctx context.Context, projectID string) ([]string, error) {
	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
        SELECT tablename
        FROM pg_tables
        WHERE schemaname = 'public'
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (d *Driver) InsertRecord(ctx context.Context, projectID string, container string, data map[string]interface{}) error {
	if !isValidIdentifier(container) {
		return fmt.Errorf("invalid container name: %s", container)
	}
	if len(data) == 0 {
		return errors.New("data cannot be empty")
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	cols := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	args := make([]interface{}, 0, len(data))
	i := 1
	for k, v := range data {
		if !isValidIdentifier(k) {
			return fmt.Errorf("invalid column name: %s", k)
		}
		cols = append(cols, fmt.Sprintf(`"%s"`, k))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		args = append(args, v)
		i++
	}

	query := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`,
		container,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err = pool.Exec(ctx, query, args...)
	return err
}

func (d *Driver) GetRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) ([]map[string]interface{}, error) {
	if !isValidIdentifier(container) {
		return nil, fmt.Errorf("invalid container name: %s", container)
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	query := fmt.Sprintf(`SELECT * FROM "%s"`, container)
	args := []interface{}{}
	if len(filter) > 0 {
		parts := make([]string, 0, len(filter))
		i := 1
		for k, v := range filter {
			if !isValidIdentifier(k) {
				return nil, fmt.Errorf("invalid column name: %s", k)
			}
			parts = append(parts, fmt.Sprintf(`"%s" = $%d`, k, i))
			args = append(args, v)
			i++
		}
		query += " WHERE " + strings.Join(parts, " AND ")
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	cols := make([]string, len(fds))
	for i, fd := range fds {
		cols[i] = string(fd.Name)
	}

	var out []map[string]interface{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		rowMap := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			rowMap[col] = values[i]
		}
		out = append(out, rowMap)
	}
	return out, rows.Err()
}

func (d *Driver) UpdateRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}, update map[string]interface{}) error {
	if !isValidIdentifier(container) {
		return fmt.Errorf("invalid container name: %s", container)
	}
	if len(update) == 0 {
		return errors.New("update cannot be empty")
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	setParts := make([]string, 0, len(update))
	whereParts := make([]string, 0, len(filter))
	args := []interface{}{}
	i := 1
	for k, v := range update {
		if !isValidIdentifier(k) {
			return fmt.Errorf("invalid column name: %s", k)
		}
		setParts = append(setParts, fmt.Sprintf(`"%s" = $%d`, k, i))
		args = append(args, v)
		i++
	}
	for k, v := range filter {
		if !isValidIdentifier(k) {
			return fmt.Errorf("invalid column name: %s", k)
		}
		whereParts = append(whereParts, fmt.Sprintf(`"%s" = $%d`, k, i))
		args = append(args, v)
		i++
	}

	query := fmt.Sprintf(`UPDATE "%s" SET %s`, container, strings.Join(setParts, ", "))
	if len(whereParts) > 0 {
		query += " WHERE " + strings.Join(whereParts, " AND ")
	}

	_, err = pool.Exec(ctx, query, args...)
	return err
}

func (d *Driver) DeleteRecords(ctx context.Context, projectID string, container string, filter map[string]interface{}) error {
	if !isValidIdentifier(container) {
		return fmt.Errorf("invalid container name: %s", container)
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	query := fmt.Sprintf(`DELETE FROM "%s"`, container)
	args := []interface{}{}
	if len(filter) > 0 {
		parts := make([]string, 0, len(filter))
		i := 1
		for k, v := range filter {
			if !isValidIdentifier(k) {
				return fmt.Errorf("invalid column name: %s", k)
			}
			parts = append(parts, fmt.Sprintf(`"%s" = $%d`, k, i))
			args = append(args, v)
			i++
		}
		query += " WHERE " + strings.Join(parts, " AND ")
	}

	_, err = pool.Exec(ctx, query, args...)
	return err
}

func (d *Driver) AddField(ctx context.Context, projectID string, container string, field string, fieldType string) error {
	if !isValidIdentifier(container) || !isValidIdentifier(field) {
		return fmt.Errorf("invalid container or field name")
	}
	if strings.TrimSpace(fieldType) == "" {
		return errors.New("fieldType is required for PostgreSQL")
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	query := fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN IF NOT EXISTS "%s" %s`, container, field, fieldType)
	_, err = pool.Exec(ctx, query)
	return err
}

func (d *Driver) RemoveField(ctx context.Context, projectID string, container string, field string) error {
	if !isValidIdentifier(container) || !isValidIdentifier(field) {
		return fmt.Errorf("invalid container or field name")
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return err
	}
	defer pool.Close()

	query := fmt.Sprintf(`ALTER TABLE "%s" DROP COLUMN IF EXISTS "%s"`, container, field)
	_, err = pool.Exec(ctx, query)
	return err
}

// ExecuteQuery expects query to be a string containing a single SQL statement.
func (d *Driver) ExecuteQuery(ctx context.Context, projectID string, query interface{}) (interface{}, error) {
	sqlStr, ok := query.(string)
	if !ok {
		return nil, fmt.Errorf("Postgres driver expects query as string")
	}

	pool, err := d.getProjectPool(ctx, projectID)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	// For simplicity, treat it as a statement without returning rows.
	_, err = pool.Exec(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

