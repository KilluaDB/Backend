package service

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (s *TableService) CreateTable(ctx context.Context, req *model.CreateTableRequest, userId uuid.UUID, projectId uuid.UUID) (*model.TableOpResult, error) {
	if err := s.validateCreateTableRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
	}
	return withProjectPool(s, ctx, userId, projectId, func(pool *pgxpool.Pool) (*model.TableOpResult, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		cmdTag, err := s.tableRepo.CreateTable(ctx, tx, req)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42P07" { // duplicate_table
				return nil, ErrTableAlreadyExists
			}
			return nil, fmt.Errorf("failed to create table: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return &model.TableOpResult{RowsAffected: cmdTag.RowsAffected()}, nil
	})
}

func (s *TableService) DeleteTable(ctx context.Context, req *model.DeleteTableRequest, userId uuid.UUID, projectId uuid.UUID) (*model.TableOpResult, error) {
	req.Schema = PostgresSchema(req.Schema)
	req.Table = strings.TrimSpace(req.Table)
	if !isValidIdentifier(req.Schema) {
		return nil, fmt.Errorf("%w: invalid schema name", ErrInvalidTableRequest)
	}
	if !isValidIdentifier(req.Table) {
		return nil, fmt.Errorf("%w: invalid table name", ErrInvalidTableRequest)
	}
	return withProjectPool(s, ctx, userId, projectId, func(pool *pgxpool.Pool) (*model.TableOpResult, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		cmdTag, err := s.tableRepo.Delete(ctx, tx, req.Schema, req.Table)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // undefined_table
				return nil, ErrTableNotFound
			}
			return nil, fmt.Errorf("failed to delete table: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return &model.TableOpResult{RowsAffected: cmdTag.RowsAffected()}, nil
	})
}

// validateCreateTableRequest validates the create table request
func (s *TableService) validateCreateTableRequest(req *model.CreateTableRequest) error {
	req.Schema = PostgresSchema(req.Schema)
	req.Table = strings.TrimSpace(req.Table)
	if !isValidIdentifier(req.Schema) {
		return errors.New("invalid schema name")
	}
	if !isValidIdentifier(req.Table) {
		return errors.New("invalid table name")
	}

	if len(req.Columns) == 0 {
		return errors.New("at least one column is required")
	}
	if err := validateTableColumnDefs(req.Columns); err != nil {
		return err
	}
	if req.ForeignKeys != nil {
		if err := validateTableForeignKeyDef(req.ForeignKeys); err != nil {
			return err
		}
	}

	return nil
}

// UpdateTable applies a partial PATCH: optional column sync, optional FK sync, optional rename/move.
// See OpenAPI for semantics (omit columns / foreign_keys to leave them unchanged).
func (s *TableService) UpdateTable(ctx context.Context, userID, projectID uuid.UUID, currentSchema, currentTable string, req *model.UpdateTableRequest) (*model.TableOpResult, error) {
	if err := ValidatePostgresSchemaName(currentSchema); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
	}
	currentSchema = PostgresSchema(currentSchema)
	if err := validateRowColumnIdentifier(currentTable); err != nil {
		return nil, fmt.Errorf("%w: invalid table name: %v", ErrInvalidTableRequest, err)
	}

	hasCol := req.Columns != nil
	hasFK := req.ForeignKeys != nil
	newTable := strings.TrimSpace(req.Table)
	newSchemaField := strings.TrimSpace(req.Schema)

	targetTable := currentTable
	if newTable != "" {
		if !isValidIdentifier(newTable) {
			return nil, fmt.Errorf("%w: invalid new table name", ErrInvalidTableRequest)
		}
		targetTable = newTable
	}
	targetSchema := currentSchema
	if newSchemaField != "" {
		if err := ValidatePostgresSchemaName(newSchemaField); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
		}
		targetSchema = PostgresSchema(newSchemaField)
	}

	renameOrMove := targetTable != currentTable || targetSchema != currentSchema
	if !hasCol && !hasFK && !renameOrMove {
		return nil, fmt.Errorf("%w: no changes", ErrInvalidTableRequest)
	}
	if hasCol && len(req.Columns) == 0 {
		return nil, fmt.Errorf("%w: empty columns list is not allowed; omit the field to leave columns unchanged", ErrInvalidTableRequest)
	}
	if hasCol {
		if err := validateTableColumnDefs(req.Columns); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
		}
	}
	if hasFK {
		if err := validateTableForeignKeyDef(req.ForeignKeys); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTableRequest, err)
		}
	}

	return withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (*model.TableOpResult, error) {
		schemaRepo := repository.NewSchemaRepository(pool)
		exists, err := schemaRepo.TableExists(ctx, currentSchema, currentTable)
		if err != nil {
			return nil, fmt.Errorf("failed to check table: %w", err)
		}
		if !exists {
			return nil, ErrTableNotFound
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		workingSchema, workingTable := currentSchema, currentTable

		if hasCol {
			details, err := schemaRepo.GetColumnDetails(ctx, workingSchema, workingTable)
			if err != nil {
				return nil, fmt.Errorf("failed to read columns: %w", err)
			}
			pkList, err := schemaRepo.GetPrimaryKeys(ctx, workingSchema, workingTable)
			if err != nil {
				return nil, fmt.Errorf("failed to read primary keys: %w", err)
			}
			pkSet := make(map[string]bool)
			for _, c := range pkList {
				pkSet[c] = true
			}
			tableHasPK := len(pkList) > 0

			pairs := make([]repository.TableColumn, 0, len(details))
			for _, d := range details {
				pairs = append(pairs, repository.TableColumn{Table: workingTable, Column: d.Name})
			}
			uniqueMap, err := schemaRepo.GetUniqueConstraintsBatch(ctx, workingSchema, pairs)
			if err != nil {
				return nil, fmt.Errorf("failed to read unique constraints: %w", err)
			}

			detailByName := make(map[string]model.ColumnDetail)
			for _, d := range details {
				detailByName[d.Name] = d
			}
			desiredSet := make(map[string]bool)
			for _, col := range req.Columns {
				desiredSet[col.Name] = true
			}

			for _, col := range req.Columns {
				if _, exists := detailByName[col.Name]; exists {
					continue
				}
				if err := s.tableRepo.AddColumnFromDefTx(ctx, tx, workingSchema, workingTable, col, tableHasPK); err != nil {
					return nil, fmt.Errorf("failed to add column %q: %w", col.Name, err)
				}
				if col.Primary {
					tableHasPK = true
				}
			}

			for _, col := range req.Columns {
				d, ok := detailByName[col.Name]
				if !ok {
					continue
				}
				if col.Primary != pkSet[col.Name] || col.IsIdentity != d.IsIdentity {
					return nil, fmt.Errorf("%w: constraint changes on existing columns are not supported; use SQL migration", ErrInvalidTableRequest)
				}
				uniqKey := fmt.Sprintf("%s:%s", workingTable, col.Name)
				if col.IsUnique != uniqueMap[uniqKey] {
					return nil, fmt.Errorf("%w: constraint changes on existing columns are not supported; use SQL migration", ErrInvalidTableRequest)
				}
				if !typesCompatibleForUpdate(col.Type, d) {
					if err := s.tableRepo.AlterColumnTypeTx(ctx, tx, workingSchema, workingTable, col.Name, col.Type); err != nil {
						return nil, fmt.Errorf("alter column %q type: %w", col.Name, err)
					}
				}
				wantNull := col.Nullable
				if d.IsNullable != wantNull {
					if err := s.tableRepo.AlterColumnNotNullTx(ctx, tx, workingSchema, workingTable, col.Name, !wantNull); err != nil {
						return nil, fmt.Errorf("alter column %q nullability: %w", col.Name, err)
					}
				}
				if !defaultsComparable(col.Default, d.ColumnDefault) {
					if err := s.tableRepo.AlterColumnDefaultTx(ctx, tx, workingSchema, workingTable, col.Name, col.Default); err != nil {
						return nil, fmt.Errorf("alter column %q default: %w", col.Name, err)
					}
				}
			}

			if hasFK {
				if err := s.syncForeignKeysTx(ctx, tx, schemaRepo, workingSchema, workingTable, req.ForeignKeys); err != nil {
					return nil, err
				}
			}

			for _, d := range details {
				if desiredSet[d.Name] {
					continue
				}
				if err := s.tableRepo.DropColumnTx(ctx, tx, workingSchema, workingTable, d.Name); err != nil {
					return nil, fmt.Errorf("failed to drop column %q: %w", d.Name, err)
				}
			}
		} else if hasFK {
			if err := s.syncForeignKeysTx(ctx, tx, schemaRepo, workingSchema, workingTable, req.ForeignKeys); err != nil {
				return nil, err
			}
		}

		if targetTable != workingTable {
			if err := s.tableRepo.RenameTableTx(ctx, tx, workingSchema, workingTable, targetTable); err != nil {
				return nil, fmt.Errorf("rename table: %w", err)
			}
			workingTable = targetTable
		}
		if targetSchema != workingSchema {
			if err := s.tableRepo.SetTableSchemaTx(ctx, tx, workingSchema, workingTable, targetSchema); err != nil {
				return nil, fmt.Errorf("set table schema: %w", err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
		return &model.TableOpResult{RowsAffected: 1}, nil
	})
}

func (s *TableService) syncForeignKeysTx(ctx context.Context, tx pgx.Tx, schemaRepo *repository.SchemaRepository, schema, table string, desired *model.TableForeignKeyDef) error {
	existing, err := schemaRepo.GetForeignKeys(ctx, schema, table)
	if err != nil {
		return fmt.Errorf("failed to read foreign keys: %w", err)
	}
	byCon := make(map[string][]model.ForeignKey)
	for _, fk := range existing {
		byCon[fk.ConstraintName] = append(byCon[fk.ConstraintName], fk)
	}

	var singles []model.ForeignKey
	for _, rows := range byCon {
		if len(rows) != 1 {
			continue
		}
		singles = append(singles, rows[0])
	}

	desiredKeys := make(map[string]bool)
	var toAdd []model.ForeignKeyRef
	refSchema := "public"
	refTable := ""
	if desired != nil && len(desired.References) > 0 {
		refSchema = PostgresSchema(desired.Schema)
		refTable = strings.TrimSpace(desired.Table)
		for _, ref := range desired.References {
			toAdd = append(toAdd, ref)
			desiredKeys[fkEdgeKey(ref.LocalColumn, refSchema, refTable, ref.ForeignColumn, ref.OnUpdate, ref.OnDelete)] = true
		}
	}

	existingKeys := make(map[string]model.ForeignKey)
	for _, e := range singles {
		toSch := e.ToSchema
		if toSch == "" {
			toSch = "public"
		}
		k := fkEdgeKey(e.FromColumn, toSch, e.ToTable, e.ToColumn, e.UpdateRule, e.DeleteRule)
		existingKeys[k] = e
	}

	for _, e := range singles {
		toSch := e.ToSchema
		if toSch == "" {
			toSch = "public"
		}
		k := fkEdgeKey(e.FromColumn, toSch, e.ToTable, e.ToColumn, e.UpdateRule, e.DeleteRule)
		if !desiredKeys[k] {
			if err := s.tableRepo.DropConstraintTx(ctx, tx, schema, table, e.ConstraintName); err != nil {
				return fmt.Errorf("drop foreign key %q: %w", e.ConstraintName, err)
			}
		}
	}

	for i, ref := range toAdd {
		k := fkEdgeKey(ref.LocalColumn, refSchema, refTable, ref.ForeignColumn, ref.OnUpdate, ref.OnDelete)
		if _, ok := existingKeys[k]; ok {
			continue
		}
		name := generateFKConstraintName(table, ref.LocalColumn, refTable, i+1)
		if err := s.tableRepo.AddForeignKeyTx(ctx, tx, schema, table, name, ref.LocalColumn, refSchema, refTable, ref.ForeignColumn, normalizeFKRule(ref.OnUpdate), normalizeFKRule(ref.OnDelete)); err != nil {
			return fmt.Errorf("add foreign key on %q: %w", ref.LocalColumn, err)
		}
	}
	return nil
}

func fkEdgeKey(localCol, refSchema, refTable, refCol, onUpdate, onDelete string) string {
	return strings.ToUpper(localCol) + "|" + strings.ToUpper(refSchema) + "|" + strings.ToUpper(refTable) + "|" + strings.ToUpper(refCol) + "|" + strings.ToUpper(normalizeFKRule(onUpdate)) + "|" + strings.ToUpper(normalizeFKRule(onDelete))
}

func normalizeFKRule(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" || s == "RESTRICT" {
		return "NO ACTION"
	}
	return s
}

func generateFKConstraintName(table, localCol, refTable string, seq int) string {
	base := fmt.Sprintf("fk_%s_%s_%s_%d", table, localCol, refTable, seq)
	if len(base) <= 63 {
		return base
	}
	return base[:63]
}

func normalizedTypeFromPGDetail(d model.ColumnDetail) string {
	dt := strings.ToLower(strings.TrimSpace(d.DataType))
	switch dt {
	case "integer":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "smallint":
		return "SMALLINT"
	case "character varying":
		return "VARCHAR"
	case "text":
		return "TEXT"
	case "boolean":
		return "BOOLEAN"
	case "double precision":
		return "DOUBLE PRECISION"
	case "real":
		return "REAL"
	case "timestamp with time zone":
		return "TIMESTAMPTZ"
	case "timestamp without time zone":
		return "TIMESTAMP"
	case "uuid":
		return "UUID"
	case "jsonb":
		return "JSONB"
	case "json":
		return "JSON"
	case "bytea":
		return "BYTEA"
	case "date":
		return "DATE"
	case "numeric":
		return "NUMERIC"
	case "interval":
		return "INTERVAL"
	default:
		return strings.ToUpper(strings.ReplaceAll(dt, " ", ""))
	}
}

func normalizeUserColumnType(t string) string {
	u := strings.TrimSpace(strings.ToUpper(t))
	if i := strings.IndexByte(u, '('); i > 0 {
		u = strings.TrimSpace(u[:i])
	}
	switch u {
	case "INT", "INT4":
		return "INTEGER"
	case "INT8":
		return "BIGINT"
	case "INT2":
		return "SMALLINT"
	case "BOOL":
		return "BOOLEAN"
	case "DECIMAL":
		return "NUMERIC"
	default:
		return u
	}
}

func physicalTypeFromDetail(d model.ColumnDetail) string {
	dt := strings.ToLower(strings.TrimSpace(d.DataType))
	switch dt {
	case "character varying":
		if d.CharMaxLength != nil {
			return fmt.Sprintf("VARCHAR(%d)", *d.CharMaxLength)
		}
		return "VARCHAR"
	case "character":
		if d.CharMaxLength != nil {
			return fmt.Sprintf("CHAR(%d)", *d.CharMaxLength)
		}
		return "CHAR"
	default:
		return normalizedTypeFromPGDetail(d)
	}
}

func typesCompatibleForUpdate(reqType string, d model.ColumnDetail) bool {
	a := strings.TrimSpace(strings.ToUpper(reqType))
	b := strings.TrimSpace(strings.ToUpper(physicalTypeFromDetail(d)))
	if a == b {
		return true
	}
	if !strings.Contains(a, "(") && !strings.Contains(b, "(") {
		return normalizeUserColumnType(reqType) == normalizeUserColumnType(physicalTypeFromDetail(d))
	}
	return false
}

func defaultsComparable(req *string, pg *string) bool {
	reqEmpty := req == nil || strings.TrimSpace(*req) == ""
	pgEmpty := pg == nil || strings.TrimSpace(*pg) == ""
	if reqEmpty && pgEmpty {
		return true
	}
	if reqEmpty || pgEmpty {
		return false
	}
	a := strings.TrimSpace(*req)
	b := strings.TrimSpace(*pg)
	if idx := strings.Index(b, "::"); idx > 0 {
		b = strings.TrimSpace(b[:idx])
	}
	b = strings.Trim(b, "()")
	a = strings.Trim(a, "'")
	b = strings.Trim(b, "'")
	return strings.EqualFold(a, b)
}
