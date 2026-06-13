package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/postgres/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Covers the switch arms not exercised by the existing TestNormalizedTypeFromPGDetail.
func TestNormalizedTypeFromPGDetail_remainingArms(t *testing.T) {
	tests := []struct{ in, want string }{
		{"real", "REAL"},
		{"bytea", "BYTEA"},
		{"interval", "INTERVAL"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizedTypeFromPGDetail(detailType(tt.in)))
		})
	}
}

// A non-duplicate exec error from CreateTable is wrapped (not mapped to ErrTableAlreadyExists).
func TestTableService_CreateTable_genericExecError(t *testing.T) {
	mock := newMockPool(t)
	mock.ExpectBegin()
	mock.ExpectExec(`CREATE TABLE`).WillReturnError(errors.New("create boom"))
	mock.ExpectRollback()

	svc := tableSvcWithMock(t, mock)
	_, err := svc.CreateTable(context.Background(), &model.CreateTableRequest{
		Table:   "users",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER"}},
	}, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrTableAlreadyExists)
	assert.Contains(t, err.Error(), "failed to create table")
}

// All these UpdateTable cases fail validation before any pool access.
func TestTableService_UpdateTable_validation(t *testing.T) {
	svc := tableSvcWithMock(t, newMockPool(t))
	ctx := context.Background()
	u, p := uuid.New(), uuid.New()

	cases := []struct {
		name          string
		currentSchema string
		currentTable  string
		req           *model.UpdateTableRequest
	}{
		{
			name: "invalid current schema", currentSchema: "bad schema", currentTable: "users",
			req: &model.UpdateTableRequest{},
		},
		{
			name: "invalid current table", currentSchema: "public", currentTable: "bad table",
			req: &model.UpdateTableRequest{},
		},
		{
			name: "invalid new table name", currentSchema: "public", currentTable: "users",
			req: &model.UpdateTableRequest{Table: "bad table"},
		},
		{
			name: "invalid new schema", currentSchema: "public", currentTable: "users",
			req: &model.UpdateTableRequest{Schema: "bad schema"},
		},
		{
			name: "invalid column def", currentSchema: "public", currentTable: "users",
			req: &model.UpdateTableRequest{Columns: []model.TableColumnDef{{Name: "bad col", Type: "TEXT"}}},
		},
		{
			name: "invalid foreign key def", currentSchema: "public", currentTable: "users",
			req: &model.UpdateTableRequest{ForeignKeys: &model.TableForeignKeyDef{
				Table:      "bad table",
				References: []model.ForeignKeyRef{{LocalColumn: "id", ForeignColumn: "id"}},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.UpdateTable(ctx, u, p, c.currentSchema, c.currentTable, c.req)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidTableRequest)
		})
	}
}

// Ensure the local-column-not-in-columns validation in validateCreateTableRequest is hit.
func TestValidateCreateTableRequest_fkLocalColumnMissing(t *testing.T) {
	svc := NewTableService(stubInstanceConn{}, nil)
	req := &model.CreateTableRequest{
		Schema:  "public",
		Table:   "orders",
		Columns: []model.TableColumnDef{{Name: "id", Type: "INTEGER", Primary: true}},
		ForeignKeys: []model.TableForeignKeyDef{{
			Table:      "users",
			References: []model.ForeignKeyRef{{LocalColumn: "user_id", ForeignColumn: "id"}},
		}},
	}
	err := svc.validateCreateTableRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined in columns")
}
