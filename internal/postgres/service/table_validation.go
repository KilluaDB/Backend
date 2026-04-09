package service

import (
	"backend/internal/postgres/model"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Compiled patterns for identifier validation (avoid per-call regexp compilation).
var (
	identifierUnquotedPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]*$`)
	// rowPathIdentifierPattern is for table/column names in row and index routes (URL path segments may include hyphens).
	rowPathIdentifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)
)

// allowedPostgresColumnTypeBases is the set of normalized type names (before parentheses) accepted for API column types.
var allowedPostgresColumnTypeBases = map[string]struct{}{
	"INT": {}, "INTEGER": {}, "BIGINT": {}, "SMALLINT": {}, "INT2": {}, "INT4": {}, "INT8": {},
	"SERIAL": {}, "BIGSERIAL": {}, "SMALLSERIAL": {},
	"DECIMAL": {}, "NUMERIC": {},
	"REAL": {}, "DOUBLE PRECISION": {}, "FLOAT": {}, "FLOAT4": {}, "FLOAT8": {},
	"BOOLEAN": {}, "BOOL": {},
	"CHAR": {}, "VARCHAR": {}, "CHARACTER": {}, "TEXT": {}, "CHARACTER VARYING": {},
	"DATE": {},
	"TIME": {}, "TIME WITH TIME ZONE": {}, "TIME WITHOUT TIME ZONE": {},
	"TIMESTAMP": {}, "TIMESTAMPTZ": {},
	"TIMESTAMP WITH TIME ZONE": {}, "TIMESTAMP WITHOUT TIME ZONE": {},
	"INTERVAL": {},
	"UUID":     {}, "JSON": {}, "JSONB": {}, "BYTEA": {},
}

// Sentinel errors for table operations so handlers can return proper HTTP status.
var (
	ErrInvalidTableRequest = errors.New("invalid table request")
	ErrTableAlreadyExists  = errors.New("table already exists")
	ErrTableNotFound       = errors.New("table does not exist")
	ErrIndexAlreadyExists  = errors.New("index already exists")
)

// isValidIdentifier checks unquoted PostgreSQL identifiers for DDL (schema, table, and column names on create/update).
// It does not allow hyphens; see validateRowColumnIdentifier for the looser rule used in row/index URL segments.
func isValidIdentifier(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	return identifierUnquotedPattern.MatchString(name)
}

// PostgresSchema returns trimmed schema, or "public" if empty (PostgreSQL default namespace).
func PostgresSchema(schema string) string {
	s := strings.TrimSpace(schema)
	if s == "" {
		return "public"
	}
	return s
}

// ValidatePostgresSchemaName returns nil if the effective schema (empty → public) is a valid PostgreSQL identifier.
func ValidatePostgresSchemaName(schema string) error {
	s := PostgresSchema(schema)
	if !isValidIdentifier(s) {
		return errors.New("invalid schema name")
	}
	return nil
}

// validateTableColumnDefs validates column names, types, and duplicates (used by create and update).
func validateTableColumnDefs(cols []model.TableColumnDef) error {
	seen := make(map[string]bool)
	for i, col := range cols {
		if !isValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name at index %d: %s", i, col.Name)
		}
		if seen[col.Name] {
			return fmt.Errorf("duplicate column name: %s", col.Name)
		}
		seen[col.Name] = true
		if col.Type == "" {
			return fmt.Errorf("column type is required for column: %s", col.Name)
		}
		if !isValidColumnType(col.Type) {
			return fmt.Errorf("invalid column type for %s: %s", col.Name, col.Type)
		}
		if col.IsIdentity && !isIdentityAllowedType(col.Type) {
			return fmt.Errorf("identity column %s must have type smallint, integer, or bigint (got %s)", col.Name, col.Type)
		}
	}
	return nil
}

// validateTableForeignKeyDef validates FK target and reference columns when references is non-empty.
func validateTableForeignKeyDef(fk *model.TableForeignKeyDef) error {
	if len(fk.References) == 0 {
		return nil
	}
	fk.Schema = PostgresSchema(fk.Schema)
	fk.Table = strings.TrimSpace(fk.Table)
	if !isValidIdentifier(fk.Schema) {
		return errors.New("invalid foreign key schema name")
	}
	if !isValidIdentifier(fk.Table) {
		return errors.New("invalid foreign key table name")
	}
	for _, ref := range fk.References {
		if !isValidIdentifier(ref.LocalColumn) || !isValidIdentifier(ref.ForeignColumn) {
			return errors.New("invalid foreign key column name")
		}
	}
	return nil
}

// columnTypeBaseForValidation returns the normalized type name before parentheses (e.g. VARCHAR(50) -> VARCHAR).
func columnTypeBaseForValidation(colType string) string {
	s := strings.TrimSpace(colType)
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "[]") {
		s = strings.TrimSpace(s[:len(s)-2])
	}
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// isValidColumnType validates PostgreSQL column types using an allowlist of normalized base type names.
func isValidColumnType(colType string) bool {
	base := columnTypeBaseForValidation(colType)
	if base == "" {
		return false
	}
	_, ok := allowedPostgresColumnTypeBases[base]
	return ok
}

// isIdentityAllowedType returns true if the column type supports GENERATED AS IDENTITY.
func isIdentityAllowedType(colType string) bool {
	switch columnTypeBaseForValidation(colType) {
	case "INT", "INTEGER", "INT2", "INT4", "SMALLINT", "BIGINT", "INT8":
		return true
	default:
		return false
	}
}

// validateRowColumnIdentifier validates table/column/index names used in row routes and similar paths.
// It is intentionally looser than isValidIdentifier: hyphens are allowed so path segments like "my-table" match
// typical URL patterns while DDL still restricts names to unquoted PostgreSQL identifiers without hyphens.
func validateRowColumnIdentifier(identifier string) error {
	if identifier == "" {
		return errors.New("identifier cannot be empty")
	}
	if !rowPathIdentifierPattern.MatchString(identifier) {
		return errors.New("invalid identifier: must start with letter or underscore and contain only alphanumeric characters, underscores, and hyphens")
	}
	return nil
}
