package repository

import (
	"backend/internal/postgres/model"
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

func appendDefaultClause(b *strings.Builder, defaultVal interface{}) {
	if defaultVal == nil {
		return
	}
	switch v := defaultVal.(type) {
	case string:
		if v == "" {
			return
		}
		escaped := strings.ReplaceAll(v, "'", "''")
		b.WriteString(fmt.Sprintf(" DEFAULT '%s'", escaped))
	case bool:
		if v {
			b.WriteString(" DEFAULT TRUE")
		} else {
			b.WriteString(" DEFAULT FALSE")
		}
	default:
		b.WriteString(fmt.Sprintf(" DEFAULT %v", v))
	}
}

type TableRepository struct{}

func NewTableRepository() *TableRepository {
	return &TableRepository{}
}

// BuildCreateTableSQL builds a CREATE TABLE statement using quoted identifiers.
func BuildCreateTableSQL(req *model.CreateTableRequest) (string, error) {
	if req.Schema == "" {
		req.Schema = "public"
	}

	totalFKs := 0
	for _, fk := range req.ForeignKeys {
		totalFKs += len(fk.References)
	}

	query := fmt.Sprintf("CREATE TABLE \"%s\".\"%s\" (\n", req.Schema, req.Table)
	for i, col := range req.Columns {
		columnDef := fmt.Sprintf("  \"%s\" %s", col.Name, col.Type)

		if col.IsIdentity {
			columnDef += " GENERATED ALWAYS AS IDENTITY"
		}

		if col.Primary {
			columnDef += " PRIMARY KEY"
		}

		if col.IsUnique {
			columnDef += " UNIQUE"
		}

		if !col.Nullable {
			columnDef += " NOT NULL"
		}

		if col.Default != nil && *col.Default != "" {
			columnDef += fmt.Sprintf(" DEFAULT %s", *col.Default)
		}

		if i < len(req.Columns)-1 || totalFKs > 0 {
			columnDef += ","
		}

		query += columnDef + "\n"
	}

	emitted := 0
	for _, fk := range req.ForeignKeys {
		if len(fk.References) == 0 {
			continue
		}
		fkSchema := fk.Schema
		if fkSchema == "" {
			fkSchema = "public"
		}
		for _, ref := range fk.References {
			fkDef := fmt.Sprintf("  FOREIGN KEY (\"%s\") REFERENCES \"%s\".\"%s\"(\"%s\")",
				ref.LocalColumn,
				fkSchema,
				fk.Table,
				ref.ForeignColumn,
			)

			if ref.OnDelete != "" {
				fkDef += " ON DELETE " + ref.OnDelete
			}

			if ref.OnUpdate != "" {
				fkDef += " ON UPDATE " + ref.OnUpdate
			}

			emitted++
			if emitted < totalFKs {
				fkDef += ","
			}

			query += fkDef + "\n"
		}
	}
	query += ");\n"

	return query, nil
}

// CreateTable executes CREATE TABLE within a transaction.
func (r *TableRepository) CreateTable(ctx context.Context, tx pgx.Tx, req *model.CreateTableRequest) (pgconn.CommandTag, error) {
	query, err := BuildCreateTableSQL(req)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return tx.Exec(ctx, query)
}

func (r *TableRepository) Delete(ctx context.Context, tx pgx.Tx, schema string, table string) (pgconn.CommandTag, error) {
	query := fmt.Sprintf("DROP TABLE \"%s\".\"%s\" CASCADE", schema, table)
	return tx.Exec(ctx, query)
}

// AddColumn runs ALTER TABLE ADD COLUMN and returns information_schema ordinal_position.
func (r *TableRepository) AddColumn(ctx context.Context, pool poolQuerier, schema, tableName, colName, colType string, defaultVal interface{}) (columnID int64, err error) {
	var qb strings.Builder
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	columnNameQuoted := pq.QuoteIdentifier(colName)
	qb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableQualified, columnNameQuoted, colType))
	appendDefaultClause(&qb, defaultVal)
	query := qb.String()
	if _, err = pool.Exec(ctx, query); err != nil {
		return 0, err
	}

	_ = pool.QueryRow(ctx, `
		SELECT ordinal_position 
		FROM information_schema.columns 
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	`, schema, tableName, colName).Scan(&columnID)
	return columnID, nil
}

// DropColumn runs ALTER TABLE DROP COLUMN.
func (r *TableRepository) DropColumn(ctx context.Context, pool poolQuerier, schema, tableName, columnName string) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	columnNameQuoted := pq.QuoteIdentifier(columnName)
	query := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableQualified, columnNameQuoted)
	_, err := pool.Exec(ctx, query)
	return err
}

// AddColumnTx runs ALTER TABLE ADD COLUMN inside a transaction.
func (r *TableRepository) AddColumnTx(ctx context.Context, tx pgx.Tx, schema, tableName, colName, colType string, defaultVal interface{}) error {
	var qb strings.Builder
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	columnNameQuoted := pq.QuoteIdentifier(colName)
	qb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableQualified, columnNameQuoted, colType))
	appendDefaultClause(&qb, defaultVal)
	_, err := tx.Exec(ctx, qb.String())
	return err
}

// DropColumnTx runs ALTER TABLE DROP COLUMN inside a transaction.
func (r *TableRepository) DropColumnTx(ctx context.Context, tx pgx.Tx, schema, tableName, columnName string) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	columnNameQuoted := pq.QuoteIdentifier(columnName)
	_, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableQualified, columnNameQuoted))
	return err
}

// AddColumnFromDefTx adds a column using a full TableColumnDef (same DDL shape as CREATE TABLE).
// If tableHasPK is true, Primary on the new column is rejected.
func (r *TableRepository) AddColumnFromDefTx(ctx context.Context, tx pgx.Tx, schema, table string, col model.TableColumnDef, tableHasPK bool) error {
	if col.Primary && tableHasPK {
		return fmt.Errorf("cannot add PRIMARY KEY column when table already has a primary key")
	}
	var sb strings.Builder
	sb.WriteString(pq.QuoteIdentifier(col.Name))
	sb.WriteString(" ")
	sb.WriteString(col.Type)
	if col.IsIdentity {
		sb.WriteString(" GENERATED ALWAYS AS IDENTITY")
	}
	if col.Primary {
		sb.WriteString(" PRIMARY KEY")
	}
	if col.IsUnique {
		sb.WriteString(" UNIQUE")
	}
	if !col.Nullable {
		sb.WriteString(" NOT NULL")
	}
	if col.Default != nil && *col.Default != "" {
		sb.WriteString(fmt.Sprintf(" DEFAULT %s", *col.Default))
	}
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table))
	q := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableQualified, sb.String())
	_, err := tx.Exec(ctx, q)
	return err
}

// AlterColumnNotNullTx sets or drops NOT NULL on a column.
func (r *TableRepository) AlterColumnNotNullTx(ctx context.Context, tx pgx.Tx, schema, tableName, columnName string, notNull bool) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	colQ := pq.QuoteIdentifier(columnName)
	clause := "SET NOT NULL"
	if !notNull {
		clause = "DROP NOT NULL"
	}
	_, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", tableQualified, colQ, clause))
	return err
}

// AlterColumnDefaultTx sets or drops a column default (expression text as in CREATE TABLE).
func (r *TableRepository) AlterColumnDefaultTx(ctx context.Context, tx pgx.Tx, schema, tableName, columnName string, def *string) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	colQ := pq.QuoteIdentifier(columnName)
	if def == nil || *def == "" {
		_, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tableQualified, colQ))
		return err
	}
	_, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tableQualified, colQ, *def))
	return err
}

// AlterColumnTypeTx changes column type using USING for a cast (PostgreSQL).
func (r *TableRepository) AlterColumnTypeTx(ctx context.Context, tx pgx.Tx, schema, tableName, columnName, newType string) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	colQ := pq.QuoteIdentifier(columnName)
	q := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
		tableQualified, colQ, newType, colQ, newType)
	_, err := tx.Exec(ctx, q)
	return err
}

// RenameTableTx renames a table within its schema.
func (r *TableRepository) RenameTableTx(ctx context.Context, tx pgx.Tx, schema, oldName, newName string) error {
	q := fmt.Sprintf("ALTER TABLE %s.%s RENAME TO %s",
		pq.QuoteIdentifier(schema), pq.QuoteIdentifier(oldName), pq.QuoteIdentifier(newName))
	_, err := tx.Exec(ctx, q)
	return err
}

// SetTableSchemaTx moves a table to another schema.
func (r *TableRepository) SetTableSchemaTx(ctx context.Context, tx pgx.Tx, currentSchema, tableName, newSchema string) error {
	q := fmt.Sprintf("ALTER TABLE %s.%s SET SCHEMA %s",
		pq.QuoteIdentifier(currentSchema), pq.QuoteIdentifier(tableName), pq.QuoteIdentifier(newSchema))
	_, err := tx.Exec(ctx, q)
	return err
}

// DropConstraintTx drops a named table constraint.
func (r *TableRepository) DropConstraintTx(ctx context.Context, tx pgx.Tx, schema, tableName, constraintName string) error {
	q := fmt.Sprintf("ALTER TABLE %s.%s DROP CONSTRAINT %s",
		pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName), pq.QuoteIdentifier(constraintName))
	_, err := tx.Exec(ctx, q)
	return err
}

// AddForeignKeyTx adds a single-column foreign key constraint.
func (r *TableRepository) AddForeignKeyTx(ctx context.Context, tx pgx.Tx, schema, tableName, constraintName, localCol, refSchema, refTable, refCol, onUpdate, onDelete string) error {
	tableQualified := fmt.Sprintf("%s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(tableName))
	ref := fmt.Sprintf("%s.%s(%s)",
		pq.QuoteIdentifier(refSchema), pq.QuoteIdentifier(refTable), pq.QuoteIdentifier(refCol))
	var qb strings.Builder
	qb.WriteString(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s",
		tableQualified, pq.QuoteIdentifier(constraintName), pq.QuoteIdentifier(localCol), ref))
	if onDelete != "" {
		qb.WriteString(" ON DELETE ")
		qb.WriteString(onDelete)
	}
	if onUpdate != "" {
		qb.WriteString(" ON UPDATE ")
		qb.WriteString(onUpdate)
	}
	_, err := tx.Exec(ctx, qb.String())
	return err
}
