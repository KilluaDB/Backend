package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/postgres/model"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableService_CreateTableIndex_validation(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	cases := []struct {
		name string
		req  *model.CreateIndexRequest
		// schema/table overrides; empty means use valid defaults
		schema string
		table  string
	}{
		{name: "nil request", req: nil, schema: "public", table: "users"},
		{name: "invalid schema", req: &model.CreateIndexRequest{Name: "i", Columns: []string{"email"}}, schema: "bad schema", table: "users"},
		{name: "invalid table", req: &model.CreateIndexRequest{Name: "i", Columns: []string{"email"}}, schema: "public", table: "bad table"},
		{name: "invalid index name", req: &model.CreateIndexRequest{Name: "bad idx", Columns: []string{"email"}}, schema: "public", table: "users"},
		{name: "no columns", req: &model.CreateIndexRequest{Name: "i", Columns: nil}, schema: "public", table: "users"},
		{name: "invalid column", req: &model.CreateIndexRequest{Name: "i", Columns: []string{"bad col"}}, schema: "public", table: "users"},
		{name: "unsupported method", req: &model.CreateIndexRequest{Name: "i", Columns: []string{"email"}, Method: "rtree"}, schema: "public", table: "users"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.CreateTableIndex(ctx, p, u, c.schema, c.table, c.req)
			require.Error(t, err)
		})
	}
}

// A non-empty, allowlisted method ("hash") exercises normalizeIndexMethod's
// non-default return and the allowlist acceptance path.
func TestTableService_CreateTableIndex_explicitMethod(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`(?s)CREATE INDEX.*hash`).
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.CreateTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users",
		&model.CreateIndexRequest{Name: "idx_email", Columns: []string{"email"}, Method: "hash"})
	require.NoError(t, err)
}

// A non-pg error from the repo is returned as-is (not mapped to ErrIndexAlreadyExists).
func TestTableService_CreateTableIndex_repoError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectExec(`CREATE`).WillReturnError(errors.New("create boom"))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.CreateTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users",
		&model.CreateIndexRequest{Name: "idx_email", Columns: []string{"email"}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrIndexAlreadyExists)
}

func TestTableService_ListTableIndexes_validation(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	_, err := svc.ListTableIndexes(ctx, p, u, "bad schema", "users")
	require.Error(t, err)

	_, err = svc.ListTableIndexes(ctx, p, u, "public", "bad table")
	require.Error(t, err)
}

func TestTableService_ListTableIndexes_repoError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT EXISTS`).
		WithArgs("public", "users").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT i\.relname.*pg_class t`).
		WithArgs("public", "users").
		WillReturnError(errors.New("list boom"))

	svc := tableSvcWithRowsMock(t, mock)
	_, err := svc.ListTableIndexes(context.Background(), uuid.New(), uuid.New(), "public", "users")
	require.Error(t, err)
}

func TestTableService_DropTableIndex_validation(t *testing.T) {
	svc := tableSvcWithRowsMock(t, newTableRowsMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	cases := []struct{ name, schema, table, index string }{
		{"invalid schema", "bad schema", "users", "idx"},
		{"invalid table", "public", "bad table", "idx"},
		{"invalid index name", "public", "users", "bad idx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := svc.DropTableIndex(ctx, p, u, c.schema, c.table, c.index)
			require.Error(t, err)
		})
	}
}

func TestTableService_DropTableIndex_repoError(t *testing.T) {
	mock := newTableRowsMockPool(t)
	mock.ExpectQuery(`(?s)SELECT ix\.indisprimary`).
		WithArgs("public", "users", "idx_email").
		WillReturnError(errors.New("drop check boom"))

	svc := tableSvcWithRowsMock(t, mock)
	err := svc.DropTableIndex(context.Background(), uuid.New(), uuid.New(), "public", "users", "idx_email")
	require.Error(t, err)
}
