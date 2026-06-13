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
