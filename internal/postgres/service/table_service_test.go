package service

import (
	"context"
	"testing"

	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTableService() *TableService {
	return NewTableService(stubInstanceConn{}, repository.NewTableRepository())
}

func TestTableService_CreateTable_validation(t *testing.T) {
	svc := newTableService()
	userID := uuid.New()
	projectID := uuid.New()

	_, err := svc.CreateTable(context.Background(), &model.CreateTableRequest{
		Table: "users",
	}, userID, projectID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)

	_, err = svc.CreateTable(context.Background(), &model.CreateTableRequest{
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "bad col", Type: "TEXT"}},
	}, userID, projectID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
}

func TestTableService_DeleteTable_validation(t *testing.T) {
	svc := newTableService()
	userID := uuid.New()
	projectID := uuid.New()

	_, err := svc.DeleteTable(context.Background(), &model.DeleteTableRequest{
		Schema: "bad-schema",
		Table:  "users",
	}, userID, projectID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
}

func TestTableService_GetTables_invalidSchema(t *testing.T) {
	svc := newTableService()
	_, err := svc.GetTables(context.Background(), uuid.New(), uuid.New(), "bad-schema")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTableRequest)
}

func TestTableService_GetTableMetadata_invalidTable(t *testing.T) {
	svc := newTableService()
	_, err := svc.GetTableMetadata(context.Background(), uuid.New(), uuid.New(), "public", "9bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid table name")
}

func TestTableService_GetRows_invalidColumn(t *testing.T) {
	svc := newTableService()
	_, err := svc.GetRows(context.Background(), uuid.New(), uuid.New(), "public", "users", map[string]interface{}{
		"bad col": "x",
	}, 10, 0, false)
	require.Error(t, err)
}

func TestValidateCreateTableRequest(t *testing.T) {
	svc := newTableService()
	err := svc.validateCreateTableRequest(&model.CreateTableRequest{
		Table:   "t",
		Columns: []model.TableColumnDef{},
	})
	assert.Error(t, err)
}

type mockTablePoolSourceTest struct {
	pool TablePoolRunner
	err  error
}

func (m *mockTablePoolSourceTest) TablePool(ctx context.Context, userID, projectID uuid.UUID) (TablePoolRunner, error) {
	return m.pool, m.err
}

func TestSetPoolSourceForTest(t *testing.T) {
	svc := newTableService()
	src := &mockTablePoolSourceTest{}

	svc.SetPoolSourceForTest(src)
	assert.NotNil(t, svc.poolSource)

	pool, err := svc.projectPool(context.Background(), uuid.New(), uuid.New())
	assert.Nil(t, pool)
	assert.NoError(t, err)

	svc.SetPoolSourceForTest(nil)
	assert.Nil(t, svc.poolSource)
}
