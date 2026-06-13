package service

import (
	"testing"

	"backend/internal/postgres/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePostgresSchemaName(t *testing.T) {
	assert.NoError(t, ValidatePostgresSchemaName(""))
	assert.NoError(t, ValidatePostgresSchemaName("public"))
	assert.Error(t, ValidatePostgresSchemaName("bad-name"))
}

func TestPostgresSchema(t *testing.T) {
	assert.Equal(t, "public", PostgresSchema(""))
	assert.Equal(t, "myschema", PostgresSchema(" myschema "))
}

func TestIsValidIdentifier(t *testing.T) {
	assert.True(t, isValidIdentifier("users"))
	assert.False(t, isValidIdentifier(""))
	assert.False(t, isValidIdentifier("bad-name"))
}

func TestValidateTableColumnDefs(t *testing.T) {
	err := validateTableColumnDefs([]model.TableColumnDef{
		{Name: "id", Type: "INTEGER"},
		{Name: "email", Type: "VARCHAR(255)"},
	})
	assert.NoError(t, err)

	err = validateTableColumnDefs([]model.TableColumnDef{
		{Name: "bad col", Type: "TEXT"},
	})
	assert.Error(t, err)
}

func TestValidateRowColumnIdentifier(t *testing.T) {
	require.NoError(t, validateRowColumnIdentifier("my-table"))
	assert.Error(t, validateRowColumnIdentifier(""))
	assert.Error(t, validateRowColumnIdentifier("9bad"))
}

func TestColumnTypeBaseForValidation(t *testing.T) {
	assert.Equal(t, "VARCHAR", columnTypeBaseForValidation("varchar(100)"))
	assert.Equal(t, "INTEGER", columnTypeBaseForValidation("integer[]"))
}

func TestValidateTableForeignKeyDef(t *testing.T) {
	t.Run("nil references returns nil", func(t *testing.T) {
		assert.NoError(t, validateTableForeignKeyDef(&model.TableForeignKeyDef{}))
	})

	t.Run("invalid schema name", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Schema: "bad-schema",
			Table:  "users",
			References: []model.ForeignKeyRef{
				{LocalColumn: "uid", ForeignColumn: "id"},
			},
		})
		assert.ErrorContains(t, err, "invalid foreign key schema name")
	})

	t.Run("invalid table name", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Table: "bad-table",
			References: []model.ForeignKeyRef{
				{LocalColumn: "uid", ForeignColumn: "id"},
			},
		})
		assert.ErrorContains(t, err, "invalid foreign key table name")
	})

	t.Run("invalid local column name", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Table: "users",
			References: []model.ForeignKeyRef{
				{LocalColumn: "bad col", ForeignColumn: "id"},
			},
		})
		assert.ErrorContains(t, err, "invalid foreign key column name")
	})

	t.Run("invalid foreign column name", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Table: "users",
			References: []model.ForeignKeyRef{
				{LocalColumn: "uid", ForeignColumn: "bad col"},
			},
		})
		assert.ErrorContains(t, err, "invalid foreign key column name")
	})

	t.Run("valid single reference", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Table: "users",
			References: []model.ForeignKeyRef{
				{LocalColumn: "uid", ForeignColumn: "id"},
			},
		})
		assert.NoError(t, err)
	})

	t.Run("valid multi-reference", func(t *testing.T) {
		err := validateTableForeignKeyDef(&model.TableForeignKeyDef{
			Table: "orders",
			References: []model.ForeignKeyRef{
				{LocalColumn: "user_id", ForeignColumn: "id"},
				{LocalColumn: "product_id", ForeignColumn: "id"},
			},
		})
		assert.NoError(t, err)
	})
}

func TestIsIdentityAllowedType(t *testing.T) {
	allowed := []string{"INT", "INTEGER", "INT2", "INT4", "SMALLINT", "BIGINT", "INT8"}
	for _, typ := range allowed {
		assert.True(t, isIdentityAllowedType(typ), "expected %q to be allowed", typ)
	}
	disallowed := []string{"TEXT", "VARCHAR", "BOOLEAN", "UUID", "JSON", "DATE", "FLOAT", "NUMERIC", "TIMESTAMP", "TIMESTAMPTZ"}
	for _, typ := range disallowed {
		assert.False(t, isIdentityAllowedType(typ), "expected %q to be disallowed", typ)
	}
}

func TestIsValidColumnType_extended(t *testing.T) {
	tests := []struct {
		typ    string
		expect bool
	}{
		{"INTEGER", true},
		{"integer", true},
		{"Integer", true},
		{"VARCHAR(255)", true},
		{"varchar(100)", true},
		{"INTEGER[]", true},
		{"TEXT[]", true},
		{"unknown_type", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			assert.Equal(t, tt.expect, isValidColumnType(tt.typ))
		})
	}
}

func TestColumnTypeBaseForValidation_edgeCases(t *testing.T) {
	assert.Equal(t, "", columnTypeBaseForValidation(""))
	assert.Equal(t, "", columnTypeBaseForValidation("[]"))
	assert.Equal(t, "", columnTypeBaseForValidation("()"))
}

func TestValidateTableColumnDefs_extended(t *testing.T) {
	t.Run("duplicate column name", func(t *testing.T) {
		err := validateTableColumnDefs([]model.TableColumnDef{
			{Name: "id", Type: "INTEGER"},
			{Name: "id", Type: "TEXT"},
		})
		assert.ErrorContains(t, err, "duplicate column name")
	})

	t.Run("empty type", func(t *testing.T) {
		err := validateTableColumnDefs([]model.TableColumnDef{
			{Name: "id", Type: ""},
		})
		assert.ErrorContains(t, err, "column type is required")
	})

	t.Run("identity with non-integer type", func(t *testing.T) {
		err := validateTableColumnDefs([]model.TableColumnDef{
			{Name: "id", Type: "TEXT", IsIdentity: true},
		})
		assert.ErrorContains(t, err, "identity column id must have type smallint, integer, or bigint")
	})
}

func TestValidateCreateTableRequest_extended(t *testing.T) {
	svc := newTableService()

	t.Run("invalid schema", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Schema:  "bad-schema",
			Table:   "t",
			Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
		})
		assert.ErrorContains(t, err, "invalid schema name")
	})

	t.Run("invalid table name", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Table:   "bad-table",
			Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
		})
		assert.ErrorContains(t, err, "invalid table name")
	})

	t.Run("zero columns", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Table:   "t",
			Columns: []model.TableColumnDef{},
		})
		assert.ErrorContains(t, err, "at least one column is required")
	})

	t.Run("bad column def", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Table:   "t",
			Columns: []model.TableColumnDef{{Name: "bad col", Type: "TEXT"}},
		})
		assert.ErrorContains(t, err, "invalid column name")
	})

	t.Run("FK references undefined local column", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Table:   "orders",
			Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
			ForeignKeys: []model.TableForeignKeyDef{
				{
					Table: "users",
					References: []model.ForeignKeyRef{
						{LocalColumn: "undefined_col", ForeignColumn: "id"},
					},
				},
			},
		})
		assert.ErrorContains(t, err, "local_column")
		assert.ErrorContains(t, err, "undefined_col")
	})

	t.Run("fully valid request", func(t *testing.T) {
		err := svc.validateCreateTableRequest(&model.CreateTableRequest{
			Table:   "orders",
			Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER", Primary: true}, {Name: "user_id", Type: "INTEGER"}},
			ForeignKeys: []model.TableForeignKeyDef{
				{
					Table: "users",
					References: []model.ForeignKeyRef{
						{LocalColumn: "user_id", ForeignColumn: "id"},
					},
				},
			},
		})
		assert.NoError(t, err)
	})
}
