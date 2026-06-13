package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/postgres/model"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCreateTableSQL(t *testing.T) {
	tests := []struct {
		name     string
		req      *model.CreateTableRequest
		checks   []string // substrings the SQL must contain
	}{
		{
			name: "basic primary + not null + default",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "users",
				Columns: []model.TableColumnDef{
					{Name: "id", Type: "INTEGER", Primary: true},
					{Name: "email", Type: "VARCHAR(255)", Nullable: false, Default: ptr("1")},
				},
			},
			checks: []string{`CREATE TABLE "public"."users"`, `"id" INTEGER PRIMARY KEY`, `"email" VARCHAR(255) NOT NULL`, `DEFAULT 1`},
		},
		{
			name: "empty schema defaults to public",
			req: &model.CreateTableRequest{
				Schema: "", Table: "t",
				Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
			},
			checks: []string{`CREATE TABLE "public"."t"`},
		},
		{
			name: "identity + unique + nullable true",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "products",
				Columns: []model.TableColumnDef{
					{Name: "id", Type: "BIGINT", IsIdentity: true, Primary: true},
					{Name: "sku", Type: "VARCHAR(50)", IsUnique: true, Nullable: true},
				},
			},
			checks: []string{
				`"id" BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`,
				`"sku" VARCHAR(50) UNIQUE`,
			},
		},
		{
			name: "all column options on one column",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "cfg",
				Columns: []model.TableColumnDef{
					{Name: "id", Type: "SERIAL", Primary: true, IsIdentity: true, IsUnique: true, Nullable: false},
				},
			},
			checks: []string{`"id" SERIAL GENERATED ALWAYS AS IDENTITY PRIMARY KEY UNIQUE NOT NULL`},
		},
		{
			name: "foreign key with on delete on update",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "orders",
				Columns: []model.TableColumnDef{{Name: "user_id", Type: "INTEGER"}},
				ForeignKeys: []model.TableForeignKeyDef{
					{
						Schema: "public", Table: "users",
						References: []model.ForeignKeyRef{
							{LocalColumn: "user_id", ForeignColumn: "id", OnDelete: "CASCADE", OnUpdate: "SET NULL"},
						},
					},
				},
			},
			checks: []string{`FOREIGN KEY ("user_id") REFERENCES "public"."users"("id")`, `ON DELETE CASCADE`, `ON UPDATE SET NULL`},
		},
		{
			name: "fk empty schema defaults to public",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "orders",
				Columns: []model.TableColumnDef{{Name: "uid", Type: "INTEGER"}},
				ForeignKeys: []model.TableForeignKeyDef{
					{
						Schema: "", Table: "users",
						References: []model.ForeignKeyRef{
							{LocalColumn: "uid", ForeignColumn: "id"},
						},
					},
				},
			},
			checks: []string{`REFERENCES "public"."users"`},
		},
		{
			name: "multiple fk refs comma placement",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "t",
				Columns: []model.TableColumnDef{{Name: "a", Type: "INT"}, {Name: "b", Type: "INT"}},
				ForeignKeys: []model.TableForeignKeyDef{
					{
						Table: "x",
						References: []model.ForeignKeyRef{
							{LocalColumn: "a", ForeignColumn: "id"},
							{LocalColumn: "b", ForeignColumn: "id"},
						},
					},
				},
			},
			checks: []string{`FOREIGN KEY ("a")`, `FOREIGN KEY ("b")`},
		},
		{
			name: "fk with empty references skipped",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "t",
				Columns: []model.TableColumnDef{{Name: "id", Type: "INT"}},
				ForeignKeys: []model.TableForeignKeyDef{
					{Table: "x", References: nil},
					{Table: "y", References: []model.ForeignKeyRef{}},
				},
			},
			checks: []string{`CREATE TABLE "public"."t"`, `"id" INT`},
		},
		{
			name: "no foreign keys",
			req: &model.CreateTableRequest{
				Schema: "public", Table: "simple",
				Columns: []model.TableColumnDef{{Name: "id", Type: "INT"}},
			},
			checks: []string{`CREATE TABLE "public"."simple"`, `"id" INT`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, err := BuildCreateTableSQL(tt.req)
			require.NoError(t, err)
			for _, c := range tt.checks {
				assert.Contains(t, sql, c)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// --- appendDefaultClause -----------------------------------------------------

func TestAppendDefaultClause(t *testing.T) {
	tests := []struct {
		name       string
		defaultVal interface{}
		want       string
	}{
		{name: "nil", defaultVal: nil, want: ""},
		{name: "empty string", defaultVal: "", want: ""},
		{name: "string", defaultVal: "active", want: " DEFAULT 'active'"},
		{name: "string with quote escaped", defaultVal: "a'b", want: " DEFAULT 'a''b'"},
		{name: "bool true", defaultVal: true, want: " DEFAULT TRUE"},
		{name: "bool false", defaultVal: false, want: " DEFAULT FALSE"},
		{name: "int", defaultVal: 5, want: " DEFAULT 5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			appendDefaultClause(&b, tt.defaultVal)
			assert.Equal(t, tt.want, b.String())
		})
	}
}

// --- CreateTable -------------------------------------------------------------

func TestTableRepository_CreateTable_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`CREATE TABLE "public"\."users"`).
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))

	repo := NewTableRepository()
	req := &model.CreateTableRequest{
		Schema:  "public",
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "integer", Primary: true}},
	}
	_, err := repo.CreateTable(context.Background(), mock, req)
	require.NoError(t, err)
}

func TestTableRepository_CreateTable_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`CREATE TABLE "public"\."users"`).
		WillReturnError(errors.New("create failed"))

	repo := NewTableRepository()
	req := &model.CreateTableRequest{
		Schema:  "public",
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "integer"}},
	}
	_, err := repo.CreateTable(context.Background(), mock, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
}

// --- Delete ------------------------------------------------------------------

func TestTableRepository_Delete_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`DROP TABLE "public"\."users" CASCADE`).
		WillReturnResult(pgxmock.NewResult("DROP TABLE", 0))

	repo := NewTableRepository()
	_, err := repo.Delete(context.Background(), mock, "public", "users")
	require.NoError(t, err)
}

func TestTableRepository_Delete_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`DROP TABLE "public"\."users" CASCADE`).
		WillReturnError(errors.New("drop failed"))

	repo := NewTableRepository()
	_, err := repo.Delete(context.Background(), mock, "public", "users")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop failed")
}

// --- AddColumnTx -------------------------------------------------------------

func TestTableRepository_AddColumnTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "age" integer`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.AddColumnTx(context.Background(), mock, "public", "users", "age", "integer", nil)
	require.NoError(t, err)
}

func TestTableRepository_AddColumnTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "age" integer`).
		WillReturnError(errors.New("add column failed"))

	repo := NewTableRepository()
	err := repo.AddColumnTx(context.Background(), mock, "public", "users", "age", "integer", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add column failed")
}

// --- DropColumnTx ------------------------------------------------------------

func TestTableRepository_DropColumnTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "age"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.DropColumnTx(context.Background(), mock, "public", "users", "age")
	require.NoError(t, err)
}

func TestTableRepository_DropColumnTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "age"`).
		WillReturnError(errors.New("drop column failed"))

	repo := NewTableRepository()
	err := repo.DropColumnTx(context.Background(), mock, "public", "users", "age")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop column failed")
}

// --- AddColumnFromDefTx ------------------------------------------------------

func TestTableRepository_AddColumnFromDefTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "age" integer`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	col := model.TableColumnDef{Name: "age", Type: "integer", Nullable: true}
	err := repo.AddColumnFromDefTx(context.Background(), mock, "public", "users", col, false)
	require.NoError(t, err)
}

func TestTableRepository_AddColumnFromDefTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "age" integer`).
		WillReturnError(errors.New("add def failed"))

	repo := NewTableRepository()
	col := model.TableColumnDef{Name: "age", Type: "integer", Nullable: true}
	err := repo.AddColumnFromDefTx(context.Background(), mock, "public", "users", col, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add def failed")
}

// --- AlterColumnNotNullTx ----------------------------------------------------

func TestTableRepository_AlterColumnNotNullTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" SET NOT NULL`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.AlterColumnNotNullTx(context.Background(), mock, "public", "users", "age", true)
	require.NoError(t, err)
}

func TestTableRepository_AlterColumnNotNullTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" DROP NOT NULL`).
		WillReturnError(errors.New("not null failed"))

	repo := NewTableRepository()
	err := repo.AlterColumnNotNullTx(context.Background(), mock, "public", "users", "age", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not null failed")
}

// --- AlterColumnDefaultTx ----------------------------------------------------

func TestTableRepository_AlterColumnDefaultTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" SET DEFAULT 0`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	def := "0"
	err := repo.AlterColumnDefaultTx(context.Background(), mock, "public", "users", "age", &def)
	require.NoError(t, err)
}

func TestTableRepository_AlterColumnDefaultTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" DROP DEFAULT`).
		WillReturnError(errors.New("default failed"))

	repo := NewTableRepository()
	err := repo.AlterColumnDefaultTx(context.Background(), mock, "public", "users", "age", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default failed")
}

// --- AlterColumnTypeTx -------------------------------------------------------

func TestTableRepository_AlterColumnTypeTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" TYPE bigint USING "age"::bigint`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.AlterColumnTypeTx(context.Background(), mock, "public", "users", "age", "bigint")
	require.NoError(t, err)
}

func TestTableRepository_AlterColumnTypeTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ALTER COLUMN "age" TYPE bigint`).
		WillReturnError(errors.New("type failed"))

	repo := NewTableRepository()
	err := repo.AlterColumnTypeTx(context.Background(), mock, "public", "users", "age", "bigint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type failed")
}

// --- RenameTableTx -----------------------------------------------------------

func TestTableRepository_RenameTableTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" RENAME TO "members"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.RenameTableTx(context.Background(), mock, "public", "users", "members")
	require.NoError(t, err)
}

func TestTableRepository_RenameTableTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" RENAME TO "members"`).
		WillReturnError(errors.New("rename failed"))

	repo := NewTableRepository()
	err := repo.RenameTableTx(context.Background(), mock, "public", "users", "members")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename failed")
}

// --- SetTableSchemaTx --------------------------------------------------------

func TestTableRepository_SetTableSchemaTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" SET SCHEMA "archive"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.SetTableSchemaTx(context.Background(), mock, "public", "users", "archive")
	require.NoError(t, err)
}

func TestTableRepository_SetTableSchemaTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" SET SCHEMA "archive"`).
		WillReturnError(errors.New("set schema failed"))

	repo := NewTableRepository()
	err := repo.SetTableSchemaTx(context.Background(), mock, "public", "users", "archive")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set schema failed")
}

// --- DropConstraintTx --------------------------------------------------------

func TestTableRepository_DropConstraintTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP CONSTRAINT "fk_user"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.DropConstraintTx(context.Background(), mock, "public", "users", "fk_user")
	require.NoError(t, err)
}

func TestTableRepository_DropConstraintTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP CONSTRAINT "fk_user"`).
		WillReturnError(errors.New("drop constraint failed"))

	repo := NewTableRepository()
	err := repo.DropConstraintTx(context.Background(), mock, "public", "users", "fk_user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop constraint failed")
}

// --- AddForeignKeyTx ---------------------------------------------------------

func TestTableRepository_AddForeignKeyTx_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."orders" ADD CONSTRAINT "fk_user" FOREIGN KEY \("user_id"\) REFERENCES "public"\."users"\("id"\)`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.AddForeignKeyTx(context.Background(), mock, "public", "orders", "fk_user", "user_id", "public", "users", "id", "", "")
	require.NoError(t, err)
}

func TestTableRepository_AddForeignKeyTx_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."orders" ADD CONSTRAINT "fk_user" FOREIGN KEY \("user_id"\) REFERENCES "public"\."users"\("id"\)`).
		WillReturnError(errors.New("add fk failed"))

	repo := NewTableRepository()
	err := repo.AddForeignKeyTx(context.Background(), mock, "public", "orders", "fk_user", "user_id", "public", "users", "id", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add fk failed")
}

func TestTableRepository_AddForeignKeyTx_withActions(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."orders" ADD CONSTRAINT "fk_u" FOREIGN KEY \("uid"\) REFERENCES "public"\."users"\("id"\) ON DELETE CASCADE ON UPDATE SET NULL`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.AddForeignKeyTx(context.Background(), mock, "public", "orders", "fk_u", "uid", "public", "users", "id", "SET NULL", "CASCADE")
	require.NoError(t, err)
}

// --- AddColumn (non-tx, uses poolQuerier) ---

func TestTableRepository_AddColumn_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "age" integer`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectQuery(`(?s)SELECT ordinal_position.*information_schema\.columns`).
		WithArgs("public", "users", "age").
		WillReturnRows(pgxmock.NewRows([]string{"ordinal_position"}).AddRow(int64(3)))

	repo := NewTableRepository()
	id, err := repo.AddColumn(context.Background(), mock, "public", "users", "age", "integer", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), id)
}

func TestTableRepository_AddColumn_withDefault(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "status" text DEFAULT 'active'`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectQuery(`(?s)SELECT ordinal_position.*information_schema\.columns`).
		WithArgs("public", "users", "status").
		WillReturnRows(pgxmock.NewRows([]string{"ordinal_position"}).AddRow(int64(5)))

	repo := NewTableRepository()
	// Use a string default that triggers appendDefaultClause's string path.
	// The function expects interface{} and the switch will handle the string.
	var defaultVal interface{} = "active"
	id, err := repo.AddColumn(context.Background(), mock, "public", "users", "status", "text", defaultVal)
	require.NoError(t, err)
	assert.Equal(t, int64(5), id)
}

func TestTableRepository_AddColumn_execError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" ADD COLUMN "x" int`).
		WillReturnError(errors.New("exec failed"))

	repo := NewTableRepository()
	_, err := repo.AddColumn(context.Background(), mock, "public", "users", "x", "int", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec failed")
}

// --- DropColumn (non-tx) ---

func TestTableRepository_DropColumn_success(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "age"`).
		WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

	repo := NewTableRepository()
	err := repo.DropColumn(context.Background(), mock, "public", "users", "age")
	require.NoError(t, err)
}

func TestTableRepository_DropColumn_error(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"\."users" DROP COLUMN "x"`).
		WillReturnError(errors.New("drop failed"))

	repo := NewTableRepository()
	err := repo.DropColumn(context.Background(), mock, "public", "users", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "drop failed")
}

// --- AddColumnFromDefTx remaining branches ---

func TestTableRepository_AddColumnFromDefTx_primaryExists(t *testing.T) {
	repo := NewTableRepository()
	col := model.TableColumnDef{Name: "id2", Type: "INTEGER", Primary: true}
	err := repo.AddColumnFromDefTx(context.Background(), newRepoMock(t), "public", "users", col, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has a primary key")
}

func TestTableRepository_AddColumnFromDefTx_withOptions(t *testing.T) {
	tests := []struct {
		name     string
		col      model.TableColumnDef
		tableHasPK bool
		sqlCheck string
	}{
		{
			name: "identity",
			col:  model.TableColumnDef{Name: "c", Type: "BIGINT", IsIdentity: true},
			sqlCheck: `ALTER TABLE "public"."t" ADD COLUMN "c" BIGINT GENERATED ALWAYS AS IDENTITY`,
		},
		{
			name: "primary no existing pk",
			col:  model.TableColumnDef{Name: "c", Type: "INT", Primary: true},
			sqlCheck: `ALTER TABLE "public"."t" ADD COLUMN "c" INT PRIMARY KEY`,
		},
		{
			name: "unique",
			col:  model.TableColumnDef{Name: "c", Type: "TEXT", IsUnique: true},
			sqlCheck: `ALTER TABLE "public"."t" ADD COLUMN "c" TEXT UNIQUE`,
		},
		{
			name: "not null",
			col:  model.TableColumnDef{Name: "c", Type: "INT", Nullable: false},
			sqlCheck: `ALTER TABLE "public"."t" ADD COLUMN "c" INT NOT NULL`,
		},
		{
			name: "default non-empty",
			col:  model.TableColumnDef{Name: "c", Type: "INT", Nullable: true, Default: ptr("42")},
			sqlCheck: `ALTER TABLE "public"."t" ADD COLUMN "c" INT DEFAULT 42`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newRepoMock(t)
			mock.ExpectExec(tt.sqlCheck).
				WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))

			repo := NewTableRepository()
			err := repo.AddColumnFromDefTx(context.Background(), mock, "public", "t", tt.col, tt.tableHasPK)
			require.NoError(t, err)
		})
	}
}

func TestTableRepository_AddColumnFromDefTx_execError(t *testing.T) {
	mock := newRepoMock(t)
	mock.ExpectExec(`ALTER TABLE "public"."t" ADD COLUMN "c" INT`).
		WillReturnError(errors.New("alter failed"))

	repo := NewTableRepository()
	err := repo.AddColumnFromDefTx(context.Background(), mock, "public", "t",
		model.TableColumnDef{Name: "c", Type: "INT"}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alter failed")
}
