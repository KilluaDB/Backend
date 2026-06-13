package service

import (
	"backend/internal/postgres/infra"
	"backend/internal/postgres/repository"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgPoolRunner is the subset of pgxpool.Pool used by TableService (mockable via pgxmock).
type pgPoolRunner interface {
	pgQueryRunner
	Begin(ctx context.Context) (pgx.Tx, error)
}

// tablePoolSource optionally overrides pool resolution for tests.
type tablePoolSource interface {
	TablePool(ctx context.Context, userID, projectID uuid.UUID) (pgPoolRunner, error)
}

type TableService struct {
	instanceConn infra.InstanceConnectionService
	tableRepo    *repository.TableRepository
	poolSource   tablePoolSource // nil in production
}

func NewTableService(
	instanceConn infra.InstanceConnectionService,
	tableRepo *repository.TableRepository,
) *TableService {
	return &TableService{
		instanceConn: instanceConn,
		tableRepo:    tableRepo,
	}
}

func (s *TableService) projectPool(ctx context.Context, userID, projectID uuid.UUID) (pgPoolRunner, error) {
	if s.poolSource != nil {
		return s.poolSource.TablePool(ctx, userID, projectID)
	}
	return s.instanceConn.GetPool(ctx, userID, projectID)
}

// withProjectPool runs fn with a shared project pool; callers must not Close the pool.
func withProjectPool[T any](s *TableService, ctx context.Context, userID, projectID uuid.UUID, fn func(pgPoolRunner) (T, error)) (T, error) {
	var zero T
	pool, err := s.projectPool(ctx, userID, projectID)
	if err != nil {
		return zero, err
	}
	return fn(pool)
}

// withProjectPoolErr runs fn with a shared project pool when only an error is returned.
func withProjectPoolErr(s *TableService, ctx context.Context, userID, projectID uuid.UUID, fn func(pgPoolRunner) error) error {
	_, err := withProjectPool(s, ctx, userID, projectID, func(pool pgPoolRunner) (struct{}, error) {
		return struct{}{}, fn(pool)
	})
	return err
}

// TablePoolRunner is the pool subset used by TableService (pgxmock satisfies this in tests).
type TablePoolRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TablePoolSource resolves the table pool for tests in other packages.
type TablePoolSource interface {
	TablePool(ctx context.Context, userID, projectID uuid.UUID) (TablePoolRunner, error)
}

// SetPoolSourceForTest injects pool resolution for handler/repository tests.
func (s *TableService) SetPoolSourceForTest(src TablePoolSource) {
	if src == nil {
		s.poolSource = nil
		return
	}
	s.poolSource = exportedTablePoolBridge{src}
}

type exportedTablePoolBridge struct {
	src TablePoolSource
}

func (b exportedTablePoolBridge) TablePool(ctx context.Context, userID, projectID uuid.UUID) (pgPoolRunner, error) {
	return b.src.TablePool(ctx, userID, projectID)
}
