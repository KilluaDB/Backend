package repository

import (
	"testing"

	"backend/internal/postgres/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCreateTableSQL(t *testing.T) {
	def := "1"
	req := &model.CreateTableRequest{
		Schema: "public",
		Table:  "users",
		Columns: []model.TableColumnDef{
			{Name: "id", Type: "INTEGER", Primary: true},
			{Name: "email", Type: "VARCHAR(255)", Nullable: false, Default: &def},
		},
	}
	sql, err := BuildCreateTableSQL(req)
	require.NoError(t, err)
	assert.Contains(t, sql, `CREATE TABLE "public"."users"`)
	assert.Contains(t, sql, `"id" INTEGER PRIMARY KEY`)
	assert.Contains(t, sql, `"email" VARCHAR(255) NOT NULL`)
}
